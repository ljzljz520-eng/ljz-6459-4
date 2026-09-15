// Package httpserver 复盘服务 REST API。
// 全部端点只读或仅写入本服务内部存储，不存在任何机床控制端点。
package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"

	"github.com/example/gear-hardening-replay/internal/annotate"
	"github.com/example/gear-hardening-replay/internal/approval"
	"github.com/example/gear-hardening-replay/internal/domain"
	"github.com/example/gear-hardening-replay/internal/ingest"
	"github.com/example/gear-hardening-replay/internal/pipeline"
	"github.com/example/gear-hardening-replay/internal/replay"
)

// Server HTTP 服务。
type Server struct {
	svc *approval.Service
	ann ingest.AnnotationStore
	mux *http.ServeMux

	mu    sync.Mutex
	grids map[string]annotate.Input // 回放场景的对齐结果缓存（前端 3D 轨迹用）
}

// New 装配路由。
func New(svc *approval.Service, ann ingest.AnnotationStore) *Server {
	s := &Server{svc: svc, ann: ann, grids: map[string]annotate.Input{}}
	m := http.NewServeMux()
	m.HandleFunc("GET /v1/healthz", s.healthz)
	m.HandleFunc("POST /v1/batches", s.createBatch)
	m.HandleFunc("GET /v1/batches/{id}", s.getBatch)
	m.HandleFunc("POST /v1/batches/{id}/approve", s.approve)
	m.HandleFunc("POST /v1/batches/{id}/bind", s.bind)
	m.HandleFunc("POST /v1/batches/{id}/inspect", s.inspect)
	m.HandleFunc("GET /v1/batches/{id}/annotations", s.annotations)
	m.HandleFunc("POST /v1/replay/{scenario}/run", s.runReplay)
	m.HandleFunc("GET /v1/replay/{scenario}/trajectory", s.trajectory)
	s.mux = m
	return s
}

// Handler 暴露 http.Handler。
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func userOf(r *http.Request) string { return r.Header.Get("X-User") }

func (s *Server) createBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	b, err := s.svc.Create(req.ID)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) getBatch(w http.ResponseWriter, r *http.Request) {
	b, err := s.svc.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	var a domain.Approval
	if !decode(w, r, &a) {
		return
	}
	if err := s.svc.Approve(r.PathValue("id"), userOf(r), a); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (s *Server) bind(w http.ResponseWriter, r *http.Request) {
	var b domain.ScanBinding
	if !decode(w, r, &b) {
		return
	}
	if err := s.svc.Bind(r.PathValue("id"), userOf(r), b); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "bound"})
}

func (s *Server) inspect(w http.ResponseWriter, r *http.Request) {
	var insp domain.Inspection
	if !decode(w, r, &insp) {
		return
	}
	if err := s.svc.Inspect(r.PathValue("id"), userOf(r), insp); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "inspected"})
}

func (s *Server) annotations(w http.ResponseWriter, r *http.Request) {
	anns, err := s.ann.ByBatch(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"annotations": anns})
}

// runReplay 运行回放场景：对齐 + 标注，结果存入标注库与轨迹缓存。
func (s *Server) runReplay(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("scenario")
	sc, ok := replay.ByName(name)
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("未知场景: "+name))
		return
	}
	data := sc.Generate()
	in, anns := pipeline.Analyze(data, annotate.Default(), 0.005)
	s.mu.Lock()
	s.grids[name] = in
	s.mu.Unlock()
	_ = s.ann.Put(r.Context(), anns)
	writeJSON(w, http.StatusOK, map[string]any{
		"batch_id":    data.BatchID,
		"scenario":    name,
		"drift_ppm":   in.DriftPPM,
		"annotations": anns,
	})
}

// trajectory 为前端 3D 回放提供对齐后的轨迹网格（可降采样）。
func (s *Server) trajectory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("scenario")
	s.mu.Lock()
	in, ok := s.grids[name]
	s.mu.Unlock()
	if !ok {
		// 未运行时即时计算
		sc, found := replay.ByName(name)
		if !found {
			writeErr(w, http.StatusNotFound, errors.New("未知场景: "+name))
			return
		}
		in, _ = pipeline.Analyze(sc.Generate(), annotate.Default(), 0.005)
	}
	step, _ := strconv.Atoi(r.URL.Query().Get("step"))
	if step < 1 {
		step = 4 // 默认 4 倍降采样（200Hz → 50Hz，约 1500 点）
	}
	g := in.G
	n := (g.N + step - 1) / step
	out := map[string]any{
		"batch_id":  in.BatchID,
		"epoch":     in.Epoch,
		"dt":        g.Dt * float64(step),
		"t":         make([]float64, 0, n),
		"z_mm":      make([]float64, 0, n),
		"theta_deg": make([]float64, 0, n),
		"power_kw":  make([]float64, 0, n),
		"freq_khz":  make([]float64, 0, n),
	}
	for i := 0; i < g.N; i += step {
		out["t"] = append(out["t"].([]float64), g.At(i))
		out["z_mm"] = append(out["z_mm"].([]float64), g.ZMM[i])
		out["theta_deg"] = append(out["theta_deg"].([]float64), g.ThetaDeg[i])
		out["power_kw"] = append(out["power_kw"].([]float64), g.PowerKW[i])
		out["freq_khz"] = append(out["freq_khz"].([]float64), g.FreqKHz[i])
	}
	writeJSON(w, http.StatusOK, out)
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, approval.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, approval.ErrRole):
		return http.StatusForbidden
	case errors.Is(err, approval.ErrState), errors.Is(err, approval.ErrIncomplete):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
