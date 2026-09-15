// Package annotate 在对齐后的统一时间网格上标注工艺异常候选。
//
// 设计原则：
//   - 输出为"候选"，供工程师复核，不直接判定废品；
//   - 每个检测器只读信号，不产生任何控制动作；
//   - 证据（evidence）随标注落库，保证回放可解释。
package annotate

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/example/gear-hardening-replay/internal/align"
	"github.com/example/gear-hardening-replay/internal/domain"
)

// Input 标注引擎输入：对齐网格 + 参考时钟事件 + 测温摘要 + 工艺程序。
type Input struct {
	BatchID  string
	Epoch    time.Time // 批次纪元（参考时钟 t=0 对应的墙钟）
	Prog     domain.Program
	G        align.Grid
	Events   []domain.MachineEvent
	Pyro     []domain.PyroSummary
	DriftPPM float64 // 对齐引擎估计的采样时钟漂移
}

// Engine 阈值参数化的标注引擎。
type Engine struct {
	EnergyRelTol   float64 // 能量密度相对偏差阈值，默认 0.15
	MinSegDurS     float64 // 异常段最短持续，默认 0.3s
	MergeGapS      float64 // 相邻段合并间隙，默认 0.2s
	HomeTolMM      float64 // 回零容差，默认 0.5mm
	QuenchMarginMS float64 // 喷液延迟裕量，默认 200ms
	EmissRelTol    float64 // 发射率失配相对阈值，默认 0.12
	FreqShiftKHz   float64 // 频率偏移阈值，默认 0.4kHz
	DriftPPMTol    float64 // 时钟漂移阈值，默认 500ppm
}

// Default 返回工程默认阈值。
func Default() Engine {
	return Engine{
		EnergyRelTol:   0.15,
		MinSegDurS:     0.3,
		MergeGapS:      0.2,
		HomeTolMM:      0.5,
		QuenchMarginMS: 200,
		EmissRelTol:    0.12,
		FreqShiftKHz:   0.4,
		DriftPPMTol:    500,
	}
}

// Run 运行全部检测器，按开始时间排序输出标注候选。
func (e Engine) Run(in Input) []domain.Annotation {
	var out []domain.Annotation
	out = append(out, e.energyDensitySteps(in)...)
	out = append(out, e.trajectoryGaps(in)...)
	out = append(out, e.homingError(in)...)
	out = append(out, e.quenchDelay(in)...)
	out = append(out, e.emissivityMismatch(in)...)
	out = append(out, e.coilGapShift(in)...)
	out = append(out, e.clockDrift(in)...)
	sort.Slice(out, func(i, j int) bool { return out[i].TStart.Before(out[j].TStart) })
	return out
}

// seg 时间网格上的异常段。
type seg struct{ i0, i1 int }

// segments 把布尔序列合并为段：短于 minDur 的丢弃，间隙小于 mergeGap 的合并。
func segments(mask []bool, dt, minDur, mergeGap float64) []seg {
	var raw []seg
	for i := 0; i < len(mask); {
		if !mask[i] {
			i++
			continue
		}
		j := i
		for j+1 < len(mask) && mask[j+1] {
			j++
		}
		raw = append(raw, seg{i, j})
		i = j + 1
	}
	if len(raw) == 0 {
		return nil
	}
	merged := []seg{raw[0]}
	for _, s := range raw[1:] {
		last := &merged[len(merged)-1]
		if float64(s.i0-last.i1-1)*dt < mergeGap {
			last.i1 = s.i1
		} else {
			merged = append(merged, s)
		}
	}
	var out []seg
	for _, s := range merged {
		if float64(s.i1-s.i0+1)*dt >= minDur {
			out = append(out, s)
		}
	}
	return out
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	ys := append([]float64(nil), xs...)
	sort.Float64s(ys)
	n := len(ys)
	if n%2 == 1 {
		return ys[n/2]
	}
	return 0.5 * (ys[n/2-1] + ys[n/2])
}

// scanMask 标记扫描段：速度足够且功率超过 50% 设定。
func scanMask(in Input) []bool {
	g := in.G
	m := make([]bool, g.N)
	thr := 0.5 * in.Prog.PowerSetKW
	for i := 0; i < g.N; i++ {
		m[i] = g.PowerKW[i] >= thr
	}
	return m
}

// speed 中心差分求扫描速度 |dz/dt|（mm/s）。
func speed(g align.Grid) []float64 {
	v := make([]float64, g.N)
	for i := 1; i < g.N-1; i++ {
		v[i] = math.Abs(g.ZMM[i+1]-g.ZMM[i-1]) / (2 * g.Dt)
	}
	if g.N >= 2 {
		v[0] = v[1]
		v[g.N-1] = v[g.N-2]
	}
	return v
}

// energyDensitySteps 能量密度突变：e = P/v（J/mm），
// 相对扫描段中位数的持续偏差，或越出工艺窗口。
func (e Engine) energyDensitySteps(in Input) []domain.Annotation {
	g := in.G
	if g.N < 3 {
		return nil
	}
	v := speed(g)
	scan := scanMask(in)
	var es []float64
	ed := make([]float64, g.N)
	for i := 0; i < g.N; i++ {
		if scan[i] && v[i] > 0.1 {
			ed[i] = g.PowerKW[i] / v[i]
			es = append(es, ed[i])
		}
	}
	if len(es) < 10 {
		return nil
	}
	base := median(es)
	if base <= 0 {
		return nil
	}
	mask := make([]bool, g.N)
	for i := 0; i < g.N; i++ {
		if !scan[i] || v[i] <= 0.1 {
			continue
		}
		rel := math.Abs(ed[i]-base) / base
		outside := in.Prog.EnergyWindowJM[1] > in.Prog.EnergyWindowJM[0] &&
			(ed[i] < in.Prog.EnergyWindowJM[0] || ed[i] > in.Prog.EnergyWindowJM[1])
		mask[i] = rel > e.EnergyRelTol || outside
	}
	var out []domain.Annotation
	for _, s := range segments(mask, g.Dt, e.MinSegDurS, e.MergeGapS) {
		maxRel := 0.0
		minE, maxE := math.Inf(1), math.Inf(-1)
		for i := s.i0; i <= s.i1; i++ {
			if !scan[i] || v[i] <= 0.1 {
				continue
			}
			if r := math.Abs(ed[i]-base) / base; r > maxRel {
				maxRel = r
			}
			minE = math.Min(minE, ed[i])
			maxE = math.Max(maxE, ed[i])
		}
		out = append(out, domain.Annotation{
			BatchID:  in.BatchID,
			TStart:   in.Epoch.Add(time.Duration(g.At(s.i0) * float64(time.Second))),
			TEnd:     in.Epoch.Add(time.Duration(g.At(s.i1) * float64(time.Second))),
			Type:     domain.AnnEnergyDensityStep,
			Severity: math.Min(1, maxRel/(2*e.EnergyRelTol)),
			Message: fmt.Sprintf("能量密度偏离基线 %.1f%%（段内 %.1f~%.1f J/mm，基线 %.1f）",
				maxRel*100, minE, maxE, base),
			Evidence: map[string]float64{
				"baseline_j_mm": base, "min_j_mm": minE, "max_j_mm": maxE,
				"max_rel_dev": maxRel,
			},
		})
	}
	return out
}

// trajectoryGaps 轨迹缺口候选：实际 Z 与程序期望轨迹的持续偏差。
// 期望轨迹：scan_start 事件起，以设定速度匀速推进至 ZEnd。
func (e Engine) trajectoryGaps(in Input) []domain.Annotation {
	g := in.G
	if g.N < 3 {
		return nil
	}
	var t0 float64
	var have bool
	for _, ev := range in.Events {
		if ev.Type == domain.EvScanStart {
			t0 = ev.Ts.Sub(in.Epoch).Seconds()
			have = true
			break
		}
	}
	if !have {
		return nil
	}
	dir := 1.0
	if in.Prog.ZEndMM < in.Prog.ZStartMM {
		dir = -1
	}
	mask := make([]bool, g.N)
	var devs []float64
	dev := make([]float64, g.N)
	for i := 0; i < g.N; i++ {
		t := g.At(i)
		zexp := in.Prog.ZStartMM + dir*in.Prog.ScanSpeedMMS*(t-t0)
		if dir > 0 {
			zexp = math.Min(zexp, in.Prog.ZEndMM)
		} else {
			zexp = math.Max(zexp, in.Prog.ZEndMM)
		}
		dev[i] = g.ZMM[i] - zexp
		// 只在扫描段内评估
		if t >= t0 && g.PowerKW[i] >= 0.5*in.Prog.PowerSetKW {
			mask[i] = math.Abs(dev[i]) > in.Prog.GapTolMM
			devs = append(devs, dev[i])
		}
	}
	var out []domain.Annotation
	for _, s := range segments(mask, g.Dt, e.MinSegDurS, e.MergeGapS) {
		maxAbs, mean, n := 0.0, 0.0, 0
		for i := s.i0; i <= s.i1; i++ {
			if !mask[i] && math.Abs(dev[i]) < in.Prog.GapTolMM {
				// 段内合并进来的小偏差点仍计入均值
			}
			maxAbs = math.Max(maxAbs, math.Abs(dev[i]))
			mean += dev[i]
			n++
		}
		if n > 0 {
			mean /= float64(n)
		}
		out = append(out, domain.Annotation{
			BatchID:  in.BatchID,
			TStart:   in.Epoch.Add(time.Duration(g.At(s.i0) * float64(time.Second))),
			TEnd:     in.Epoch.Add(time.Duration(g.At(s.i1) * float64(time.Second))),
			Type:     domain.AnnTrajectoryGap,
			Severity: math.Min(1, maxAbs/(3*in.Prog.GapTolMM)),
			Message: fmt.Sprintf("实际轨迹偏离程序期望：均值 %.2f mm，峰值 %.2f mm（容差 %.2f）",
				mean, maxAbs, in.Prog.GapTolMM),
			Evidence: map[string]float64{"mean_dev_mm": mean, "max_abs_dev_mm": maxAbs},
		})
	}
	return out
}

// homingError 轴回零错误：homing_done 事件报告的 Z 读数偏离零点。
func (e Engine) homingError(in Input) []domain.Annotation {
	for _, ev := range in.Events {
		if ev.Type != domain.EvHomingDone {
			continue
		}
		z, err := strconv.ParseFloat(ev.Payload["reported_z_mm"], 64)
		if err != nil {
			continue
		}
		if math.Abs(z) > e.HomeTolMM {
			return []domain.Annotation{{
				BatchID:  in.BatchID,
				TStart:   ev.Ts,
				TEnd:     ev.Ts,
				Type:     domain.AnnHomingError,
				Severity: math.Min(1, math.Abs(z)/(4*e.HomeTolMM)),
				Message:  fmt.Sprintf("回零完成后 Z 读数 %.3f mm，超出容差 ±%.2f mm", z, e.HomeTolMM),
				Evidence: map[string]float64{"reported_z_mm": z},
			}}
		}
	}
	return nil
}

// quenchDelay 喷液阀延迟：quench_cmd 到 quench_flow 的实测滞后
// 超过程序设定延迟 + 裕量。
func (e Engine) quenchDelay(in Input) []domain.Annotation {
	var cmd *domain.MachineEvent
	for i := range in.Events {
		ev := &in.Events[i]
		switch ev.Type {
		case domain.EvQuenchCmd:
			cmd = ev
		case domain.EvQuenchFlow:
			if cmd == nil {
				continue
			}
			lagMS := float64(ev.Ts.Sub(cmd.Ts)) / float64(time.Millisecond)
			limit := in.Prog.QuenchDelayMS + e.QuenchMarginMS
			if lagMS > limit {
				return []domain.Annotation{{
					BatchID:  in.BatchID,
					TStart:   cmd.Ts,
					TEnd:     ev.Ts,
					Type:     domain.AnnQuenchDelay,
					Severity: math.Min(1, (lagMS-limit)/(2*limit)),
					Message: fmt.Sprintf("喷液阀实测滞后 %.0f ms，超过限值 %.0f ms（设定 %.0f + 裕量 %.0f）",
						lagMS, limit, in.Prog.QuenchDelayMS, e.QuenchMarginMS),
					Evidence: map[string]float64{"lag_ms": lagMS, "limit_ms": limit},
				}}
			}
			cmd = nil
		}
	}
	return nil
}

// emissivityMismatch 测温发射率设错：
// 仪器按 ε_set 折算辐射温度，满足 ε_set·(T_read⁴-T_amb⁴)=ε_true·(T_true⁴-T_amb⁴)。
// 用标定升温系数 K（°C/kJ）从累积能量预测 T_true，
// 比值 r = (T_read⁴-T_amb⁴)/(T_pred⁴-T_amb⁴) 的中位数应≈1，否则 ε_true≈r·ε_set。
func (e Engine) emissivityMismatch(in Input) []domain.Annotation {
	g := in.G
	if g.N < 3 || len(in.Pyro) < 3 || in.Prog.TempCalibK <= 0 {
		return nil
	}
	// 累积能量 E(t) = ∫P dt（kJ）
	cum := make([]float64, g.N)
	for i := 1; i < g.N; i++ {
		cum[i] = cum[i-1] + 0.5*(g.PowerKW[i]+g.PowerKW[i-1])*g.Dt
	}
	tambK := in.Prog.AmbientC + 273.15
	var ratios []float64
	for _, p := range in.Pyro {
		t := p.Ts.Sub(in.Epoch).Seconds()
		idx := int((t - g.T0) / g.Dt)
		if idx < 0 || idx >= g.N {
			continue
		}
		tPred := in.Prog.AmbientC + in.Prog.TempCalibK*cum[idx] // °C
		predK := tPred + 273.15
		readK := p.TempC + 273.15
		num := math.Pow(readK, 4) - math.Pow(tambK, 4)
		den := math.Pow(predK, 4) - math.Pow(tambK, 4)
		if den <= 0 || num <= 0 {
			continue
		}
		ratios = append(ratios, num/den)
	}
	if len(ratios) < 3 {
		return nil
	}
	r := median(ratios)
	if math.Abs(r-1) <= e.EmissRelTol {
		return nil
	}
	est := r * in.Prog.EmissivitySet
	return []domain.Annotation{{
		BatchID:  in.BatchID,
		TStart:   in.Pyro[0].Ts,
		TEnd:     in.Pyro[len(in.Pyro)-1].Ts,
		Type:     domain.AnnEmissivity,
		Severity: math.Min(1, math.Abs(r-1)/(3*e.EmissRelTol)),
		Message: fmt.Sprintf("测温与能量平衡失配：辐射比 %.2f（应≈1），发射率设定 %.2f，估计真实值 ≈%.2f",
			r, in.Prog.EmissivitySet, est),
		Evidence: map[string]float64{
			"radiation_ratio": r, "emissivity_set": in.Prog.EmissivitySet, "emissivity_est": est,
		},
	}}
}

// coilGapShift 线圈间隙变化：频率中值偏移与能量密度同步下降联合判据
// （间隙增大 → 耦合变弱 → 匹配频率偏移、有效功率下降）。
func (e Engine) coilGapShift(in Input) []domain.Annotation {
	g := in.G
	if g.N < 3 {
		return nil
	}
	scan := scanMask(in)
	v := speed(g)
	var fBase, eBase []float64
	for i := 0; i < g.N; i++ {
		if scan[i] {
			fBase = append(fBase, g.FreqKHz[i])
			if v[i] > 0.1 {
				eBase = append(eBase, g.PowerKW[i]/v[i])
			}
		}
	}
	if len(fBase) < 10 || len(eBase) < 10 {
		return nil
	}
	f0 := median(fBase)
	e0 := median(eBase)
	// 滑动窗口（1s）检测频率偏移 + 能量下降同时成立的段
	win := int(1.0 / g.Dt)
	if win < 1 {
		win = 1
	}
	mask := make([]bool, g.N)
	var fShift, eDrop []float64
	for i := 0; i < g.N; i++ {
		if !scan[i] {
			continue
		}
		lo := i - win/2
		if lo < 0 {
			lo = 0
		}
		hi := i + win/2
		if hi >= g.N {
			hi = g.N - 1
		}
		var fs, es []float64
		for j := lo; j <= hi; j++ {
			if scan[j] {
				fs = append(fs, g.FreqKHz[j])
				if v[j] > 0.1 {
					es = append(es, g.PowerKW[j]/v[j])
				}
			}
		}
		if len(fs) == 0 || len(es) == 0 {
			continue
		}
		df := median(fs) - f0
		de := (median(es) - e0) / e0
		if math.Abs(df) > e.FreqShiftKHz && de < -0.10 {
			mask[i] = true
			fShift = append(fShift, df)
			eDrop = append(eDrop, de)
		}
	}
	var out []domain.Annotation
	for _, s := range segments(mask, g.Dt, e.MinSegDurS, e.MergeGapS) {
		maxF, minE := 0.0, 0.0
		for i := s.i0; i <= s.i1; i++ {
			if !scan[i] {
				continue
			}
			df := g.FreqKHz[i] - f0
			if math.Abs(df) > math.Abs(maxF) {
				maxF = df
			}
			if v[i] > 0.1 {
				de := (g.PowerKW[i]/v[i] - e0) / e0
				minE = math.Min(minE, de)
			}
		}
		out = append(out, domain.Annotation{
			BatchID:  in.BatchID,
			TStart:   in.Epoch.Add(time.Duration(g.At(s.i0) * float64(time.Second))),
			TEnd:     in.Epoch.Add(time.Duration(g.At(s.i1) * float64(time.Second))),
			Type:     domain.AnnCoilGapShift,
			Severity: math.Min(1, math.Abs(maxF)/(2*e.FreqShiftKHz)),
			Message: fmt.Sprintf("频率偏移 %.2f kHz 且能量密度下降 %.1f%%，疑似线圈间隙变化（标称 %.1f mm）",
				maxF, -minE*100, in.Prog.GapNominalMM),
			Evidence: map[string]float64{"freq_shift_khz": maxF, "energy_drop": minE},
		})
	}
	return out
}

// clockDrift 高频采样时钟漂移：对齐引擎拟合的漂移率超阈值。
// 对齐已校正时间轴，此标注用于追溯采集卡/晶振问题。
func (e Engine) clockDrift(in Input) []domain.Annotation {
	if math.Abs(in.DriftPPM) <= e.DriftPPMTol {
		return nil
	}
	return []domain.Annotation{{
		BatchID:  in.BatchID,
		TStart:   in.Epoch,
		TEnd:     in.Epoch,
		Type:     domain.AnnClockDrift,
		Severity: math.Min(1, math.Abs(in.DriftPPM)/(4*e.DriftPPMTol)),
		Message:  fmt.Sprintf("高频采样时钟漂移 %.0f ppm，已按仿射模型校正；建议检查采集卡时基", in.DriftPPM),
		Evidence: map[string]float64{"drift_ppm": in.DriftPPM},
	}}
}
