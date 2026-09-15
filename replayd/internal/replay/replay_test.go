package replay_test

import (
	"testing"

	"github.com/example/gear-hardening-replay/internal/annotate"
	"github.com/example/gear-hardening-replay/internal/domain"
	"github.com/example/gear-hardening-replay/internal/pipeline"
	"github.com/example/gear-hardening-replay/internal/replay"
)

// TestScenarios 每个回放场景必须产生期望的标注候选（子集断言），
// 基线场景必须零标注（无误报）。
func TestScenarios(t *testing.T) {
	for _, sc := range replay.All() {
		t.Run(sc.Name(), func(t *testing.T) {
			data := sc.Generate()
			_, anns := pipeline.Analyze(data, annotate.Default(), 0.005)
			got := map[domain.AnnotationType]bool{}
			for _, a := range anns {
				got[a.Type] = true
				t.Logf("  [%s] %s ~ %s: %s", a.Type,
					a.TStart.Format("15:04:05.000"), a.TEnd.Format("15:04:05.000"), a.Message)
			}
			for _, want := range sc.Expect() {
				if !got[want] {
					t.Errorf("场景 %s 缺少期望标注 %s，实际标注类型: %v", sc.Name(), want, keys(got))
				}
			}
			// 精确集合：不允许期望之外的标注类型（防误报回归）
			wantSet := map[domain.AnnotationType]bool{}
			for _, w := range sc.Expect() {
				wantSet[w] = true
			}
			for typ := range got {
				if !wantSet[typ] {
					t.Errorf("场景 %s 出现期望之外的标注 %s（误报）", sc.Name(), typ)
				}
			}
		})
	}
}

// TestClockDriftCorrected 时钟漂移场景：对齐校正后不应出现轨迹缺口，
// 且漂移量估计接近注入值（2000ppm，符号相反）。
func TestClockDriftCorrected(t *testing.T) {
	sc, _ := replay.ByName("clock_drift")
	in, anns := pipeline.Analyze(sc.Generate(), annotate.Default(), 0.005)
	for _, a := range anns {
		if a.Type == domain.AnnTrajectoryGap {
			t.Errorf("时钟漂移经对齐校正后不应报轨迹缺口: %s", a.Message)
		}
	}
	if in.DriftPPM > -1500 || in.DriftPPM < -2500 {
		t.Errorf("漂移估计应约 -2000ppm，实际 %.0f ppm", in.DriftPPM)
	}
	t.Logf("漂移估计 %.0f ppm（注入 +2000ppm，校正方向相反）", in.DriftPPM)
}

// TestHomingErrorEvidence 回零错误标注应携带读数证据。
func TestHomingErrorEvidence(t *testing.T) {
	sc, _ := replay.ByName("homing_error")
	_, anns := pipeline.Analyze(sc.Generate(), annotate.Default(), 0.005)
	for _, a := range anns {
		if a.Type == domain.AnnHomingError {
			if z := a.Evidence["reported_z_mm"]; z != 4.2 {
				t.Errorf("回零错误证据 reported_z_mm 应为 4.2，实际 %.3f", z)
			}
			return
		}
	}
	t.Fatal("缺少 homing_error 标注")
}

// TestQuenchDelayEvidence 喷液延迟标注应携带滞后毫秒数。
func TestQuenchDelayEvidence(t *testing.T) {
	sc, _ := replay.ByName("quench_delay")
	_, anns := pipeline.Analyze(sc.Generate(), annotate.Default(), 0.005)
	for _, a := range anns {
		if a.Type == domain.AnnQuenchDelay {
			lag := a.Evidence["lag_ms"]
			if lag < 800 || lag > 900 {
				t.Errorf("喷液滞后应约 850ms，实际 %.0fms", lag)
			}
			return
		}
	}
	t.Fatal("缺少 quench_valve_delay 标注")
}

// TestEmissivityEstimate 发射率标注应估算出接近真实值 0.35。
func TestEmissivityEstimate(t *testing.T) {
	sc, _ := replay.ByName("emissivity")
	_, anns := pipeline.Analyze(sc.Generate(), annotate.Default(), 0.005)
	for _, a := range anns {
		if a.Type == domain.AnnEmissivity {
			est := a.Evidence["emissivity_est"]
			if est < 0.30 || est > 0.40 {
				t.Errorf("发射率估计应约 0.35，实际 %.3f", est)
			}
			t.Logf("发射率估计 %.3f（真实 0.35，误设 0.90）", est)
			return
		}
	}
	t.Fatal("缺少 emissivity_mismatch 标注")
}

func keys(m map[domain.AnnotationType]bool) []domain.AnnotationType {
	var out []domain.AnnotationType
	for k := range m {
		out = append(out, k)
	}
	return out
}
