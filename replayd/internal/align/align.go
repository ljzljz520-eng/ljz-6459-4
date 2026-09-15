// Package align 把多时钟源信号对齐到统一参考时间轴。
//
// 时钟拓扑：机床事件（NATS，CNC 时钟）为参考时钟；高频功率/轴位置
// 采样（采集卡时钟）相对参考时钟存在固定偏移与线性漂移（晶振 ppm 级）。
// 对齐方法：用跨时钟域的"同一物理事件"做锚点（如 scan_start 事件
// 与功率首次越过阈值的时刻），最小二乘拟合仿射模型 tRef = α·tSrc + β。
package align

import (
	"math"
	"sort"
	"time"

	"github.com/example/gear-hardening-replay/internal/domain"
)

// ClockModel 仿射时钟模型：tRef = Alpha·tSrc + Beta（秒，相对批次纪元）。
type ClockModel struct {
	Alpha float64 // 漂移率，理想为 1
	Beta  float64 // 固定偏移（秒）
}

// Apply 把源时钟秒数换算到参考时钟。
func (m ClockModel) Apply(tSrc float64) float64 { return m.Alpha*tSrc + m.Beta }

// DriftPPM 漂移率，单位 ppm。
func (m ClockModel) DriftPPM() float64 { return (m.Alpha - 1.0) * 1e6 }

// FitClock 对锚点对 (tSrc, tRef) 做最小二乘拟合。
// 锚点不足 2 个时退化为仅偏移校正（Alpha=1）。
func FitClock(pairs [][2]float64) ClockModel {
	if len(pairs) == 0 {
		return ClockModel{Alpha: 1}
	}
	if len(pairs) == 1 {
		return ClockModel{Alpha: 1, Beta: pairs[0][1] - pairs[0][0]}
	}
	var sx, sy, sxx, sxy float64
	n := float64(len(pairs))
	for _, p := range pairs {
		sx += p[0]
		sy += p[1]
		sxx += p[0] * p[0]
		sxy += p[0] * p[1]
	}
	den := n*sxx - sx*sx
	if math.Abs(den) < 1e-12 {
		return ClockModel{Alpha: 1, Beta: sy/n - sx/n}
	}
	alpha := (n*sxy - sx*sy) / den
	beta := (sy - alpha*sx) / n
	return ClockModel{Alpha: alpha, Beta: beta}
}

// Anchors 从事件流与功率流提取跨时钟锚点对。
//
// scan_start（CNC 时钟）↔ 功率首次越过 50% 设定值（采集时钟）；
// scan_end（CNC 时钟）↔ 功率跌回 50% 以下（采集时钟）。
// 返回的每对为 (tSrc采集秒, tRef参考秒)，均相对各自时钟的批次纪元。
func Anchors(events []domain.MachineEvent, pw []domain.PowerSample, epochRef, epochSrc time.Time, setKW float64) [][2]float64 {
	thr := 0.5 * setKW
	var rise, fall float64
	var haveRise, haveFall bool
	for i, s := range pw {
		t := s.Ts.Sub(epochSrc).Seconds()
		if !haveRise && s.PowerKW >= thr {
			rise = t
			haveRise = true
		}
		if haveRise && s.PowerKW < thr && i > 0 && pw[i-1].PowerKW >= thr {
			fall = t
			haveFall = true
		}
	}
	var pairs [][2]float64
	for _, ev := range events {
		te := ev.Ts.Sub(epochRef).Seconds()
		switch ev.Type {
		case domain.EvScanStart:
			if haveRise {
				pairs = append(pairs, [2]float64{rise, te})
			}
		case domain.EvScanEnd:
			if haveFall {
				pairs = append(pairs, [2]float64{fall, te})
			}
		}
	}
	return pairs
}

// Grid 对齐后的统一时间网格（参考时钟）。
type Grid struct {
	T0       float64 // 网格起点（参考时钟秒，相对批次纪元）
	Dt       float64 // 步长（秒）
	N        int
	PowerKW  []float64
	FreqKHz  []float64
	ZMM      []float64
	ThetaDeg []float64
}

// At 返回第 i 点的参考时刻（秒）。
func (g Grid) At(i int) float64 { return g.T0 + float64(i)*g.Dt }

// Epoch 返回网格纪元对应的墙钟时间。
func (g Grid) TimeAt(epoch time.Time, i int) time.Time {
	return epoch.Add(time.Duration(g.At(i) * float64(time.Second)))
}

// Resample 把功率与轴位置流按时钟模型校正后重采样到统一网格。
// 网格范围取两流在参考时钟下的交集，线性插值。
func Resample(pw []domain.PowerSample, ax []domain.AxisSample, cm ClockModel, epochSrc time.Time, dt float64) Grid {
	type pt struct{ t, v1, v2 float64 }
	pts := make([]pt, 0, len(pw))
	for _, s := range pw {
		pts = append(pts, pt{cm.Apply(s.Ts.Sub(epochSrc).Seconds()), s.PowerKW, s.FreqKHz})
	}
	type at struct{ t, z, th float64 }
	ats := make([]at, 0, len(ax))
	for _, s := range ax {
		ats = append(ats, at{cm.Apply(s.Ts.Sub(epochSrc).Seconds()), s.ZMM, s.ThetaDeg})
	}
	if len(pts) < 2 || len(ats) < 2 {
		return Grid{}
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].t < pts[j].t })
	sort.Slice(ats, func(i, j int) bool { return ats[i].t < ats[j].t })
	lo := math.Max(pts[0].t, ats[0].t)
	hi := math.Min(pts[len(pts)-1].t, ats[len(ats)-1].t)
	if hi <= lo {
		return Grid{}
	}
	n := int((hi-lo)/dt) + 1
	g := Grid{T0: lo, Dt: dt, N: n}
	g.PowerKW = make([]float64, n)
	g.FreqKHz = make([]float64, n)
	g.ZMM = make([]float64, n)
	g.ThetaDeg = make([]float64, n)
	interp := func(xs []float64, ys []float64, x float64) float64 {
		j := sort.SearchFloat64s(xs, x)
		if j <= 0 {
			return ys[0]
		}
		if j >= len(xs) {
			return ys[len(ys)-1]
		}
		x0, x1 := xs[j-1], xs[j]
		y0, y1 := ys[j-1], ys[j]
		if x1 == x0 {
			return y0
		}
		return y0 + (y1-y0)*(x-x0)/(x1-x0)
	}
	px := make([]float64, len(pts))
	p1 := make([]float64, len(pts))
	p2 := make([]float64, len(pts))
	for i, p := range pts {
		px[i], p1[i], p2[i] = p.t, p.v1, p.v2
	}
	axs := make([]float64, len(ats))
	az := make([]float64, len(ats))
	ath := make([]float64, len(ats))
	for i, a := range ats {
		axs[i], az[i], ath[i] = a.t, a.z, a.th
	}
	for i := 0; i < n; i++ {
		t := g.At(i)
		g.PowerKW[i] = interp(px, p1, t)
		g.FreqKHz[i] = interp(px, p2, t)
		g.ZMM[i] = interp(axs, az, t)
		g.ThetaDeg[i] = interp(axs, ath, t)
	}
	return g
}
