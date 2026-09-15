// Package replay 生成回放场景：基线 + 五类典型工艺故障注入。
//
// 场景用于：
//   - 回归测试标注引擎（每类故障必须被对应检测器捕获）；
//   - 演示与培训：前端可加载场景数据回放齿面轨迹；
//   - 阈值标定：调整 Engine 参数后重放验证误报/漏报。
package replay

import (
	"math"
	"strconv"
	"time"

	"github.com/example/gear-hardening-replay/internal/domain"
)

// Scenario 一个回放场景。
type Scenario interface {
	Name() string
	Generate() domain.BatchData
	Expect() []domain.AnnotationType // 期望被标注的类型（子集断言）
}

// All 返回全部内置场景。
func All() []Scenario {
	return []Scenario{
		baseline{},
		homingError{},
		coilGap{},
		emissivity{},
		quenchDelay{},
		clockDrift{},
	}
}

// ByName 按名查找场景。
func ByName(name string) (Scenario, bool) {
	for _, s := range All() {
		if s.Name() == name {
			return s, true
		}
	}
	return nil, false
}

var epoch = time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)

func dur(t float64) time.Duration { return time.Duration(t * float64(time.Second)) }

func baseProgram() domain.Program {
	return domain.Program{
		Version:        "IH-42.3",
		ZStartMM:       10,
		ZEndMM:         110,
		ScanSpeedMMS:   5,
		PowerSetKW:     80,
		FreqSetKHz:     10,
		QuenchDelayMS:  150,
		EnergyWindowJM: [2]float64{13, 19}, // 基线 e = 80/5 = 16 J/mm
		GapNominalMM:   2.0,
		GapTolMM:       1.5,
		EmissivitySet:  0.6,
		RotRPM:         60,
		TempCalibK:     0.5, // °C/kJ
		AmbientC:       25,
	}
}

// profile 信号剖面：场景通过覆写这些钩子注入故障。
type profile struct {
	power      func(t float64) (kw, khz float64) // 功率/频率剖面
	zpos       func(t float64) float64           // Z 位置剖面
	homeZ      float64                           // homing_done 报告的 Z 读数
	quenchLagS float64                           // quench_cmd → quench_flow 滞后
	emissTrue  float64                           // 工件真实发射率（测温折算用）
	srcStretch float64                           // 采集时钟拉伸系数（1.0 = 无漂移）
}

func baseProfile() profile {
	return profile{
		power: func(t float64) (float64, float64) {
			if t < 5 || t > 25 {
				return 0, 10
			}
			return 80 * (1 + 0.01*math.Sin(2*math.Pi*7*t)), 10
		},
		zpos: func(t float64) float64 {
			switch {
			case t < 5:
				return 10
			case t > 25:
				return 110
			default:
				return 10 + 5*(t-5)
			}
		},
		homeZ:      0,
		quenchLagS: 0.150,
		emissTrue:  0.6,
		srcStretch: 1.0,
	}
}

// build 按剖面生成一批次完整信号。
// 温度场与功率剖面自洽：T_true 由实际功率的数值积分推算，
// 因此功率异常（如线圈间隙）会如实反映在升温曲线上。
func build(id string, prog domain.Program, pr profile) domain.BatchData {
	d := domain.BatchData{BatchID: id, Epoch: epoch, Program: prog}
	const dt = 0.005 // 200Hz 高频采样
	// 第一遍：生成高频流，同时在真实时间轴上积分累积能量
	var eCum []float64
	e, prevKW, prevT := 0.0, 0.0, 0.0
	for t := 0.0; t <= 30.0001; t += dt {
		kw, khz := pr.power(t)
		e += 0.5 * (kw + prevKW) * (t - prevT) // kJ
		prevKW, prevT = kw, t
		eCum = append(eCum, e)
		ts := epoch.Add(dur(t * pr.srcStretch)) // 采集时钟（可漂移）
		d.Power = append(d.Power, domain.PowerSample{Ts: ts, PowerKW: kw, FreqKHz: khz})
		d.Axis = append(d.Axis, domain.AxisSample{
			Ts: ts, ZMM: pr.zpos(t), ThetaDeg: math.Mod(360*prog.RotRPM/60*t, 360),
		})
	}
	energyAt := func(t float64) float64 {
		idx := int(t / dt)
		if idx >= len(eCum) {
			idx = len(eCum) - 1
		}
		return eCum[idx]
	}
	// 机床事件（参考时钟）
	ev := func(t float64, typ string, payload map[string]string) domain.MachineEvent {
		return domain.MachineEvent{Ts: epoch.Add(dur(t)), Source: "cnc", Type: typ, Payload: payload}
	}
	d.Events = []domain.MachineEvent{
		ev(2.0, domain.EvHomingDone, map[string]string{"reported_z_mm": fmt3(pr.homeZ)}),
		ev(5.0, domain.EvScanStart, nil),
		ev(25.0, domain.EvScanEnd, nil),
		ev(25.0+prog.QuenchDelayMS/1000, domain.EvQuenchCmd, nil),
		ev(25.0+prog.QuenchDelayMS/1000+pr.quenchLagS, domain.EvQuenchFlow, nil),
		ev(27.0, domain.EvProgramEnd, nil),
	}
	// 红外测温摘要（1Hz，加热段）：T_true 由能量积分推算，读数按发射率折算
	tambK := prog.AmbientC + 273.15
	for t := 5.5; t <= 24.5; t += 1.0 {
		tTrue := prog.AmbientC + prog.TempCalibK*energyAt(t)
		tTrueK := tTrue + 273.15
		// 仪器按 ε_set 折算：ε_set·(T_read⁴-Tamb⁴) = ε_true·(T_true⁴-Tamb⁴)
		ratio := pr.emissTrue / prog.EmissivitySet
		tReadK := math.Pow(math.Pow(tambK, 4)+ratio*(math.Pow(tTrueK, 4)-math.Pow(tambK, 4)), 0.25)
		d.Pyro = append(d.Pyro, domain.PyroSummary{
			Ts: epoch.Add(dur(t)), Channel: "IR-1",
			TempC: tReadK - 273.15, Emissivity: prog.EmissivitySet,
		})
	}
	return d
}

func fmt3(f float64) string {
	return strconv.FormatFloat(f, 'f', 3, 64)
}

// ---- 场景定义 ----

type baseline struct{}

func (baseline) Name() string                    { return "baseline" }
func (baseline) Expect() []domain.AnnotationType { return nil }
func (baseline) Generate() domain.BatchData {
	return build("B-2026-0001", baseProgram(), baseProfile())
}

// homingError 轴回零错误：回零后 Z 读数 +4.2mm，坐标系整体偏移。
type homingError struct{}

func (homingError) Name() string { return "homing_error" }
func (homingError) Expect() []domain.AnnotationType {
	return []domain.AnnotationType{domain.AnnHomingError, domain.AnnTrajectoryGap}
}
func (homingError) Generate() domain.BatchData {
	pr := baseProfile()
	pr.homeZ = 4.2
	base := pr.zpos
	pr.zpos = func(t float64) float64 { return base(t) + 4.2 }
	return build("B-2026-0002", baseProgram(), pr)
}

// coilGap 线圈间隙变化：12~20s 间隙 2.0→3.5mm，
// 匹配频率 10→11.3kHz，耦合下降致有效功率 80→58kW。
type coilGap struct{}

func (coilGap) Name() string { return "coil_gap" }
func (coilGap) Expect() []domain.AnnotationType {
	return []domain.AnnotationType{domain.AnnCoilGapShift, domain.AnnEnergyDensityStep}
}
func (coilGap) Generate() domain.BatchData {
	pr := baseProfile()
	base := pr.power
	pr.power = func(t float64) (float64, float64) {
		kw, khz := base(t)
		if t >= 12 && t <= 20 && kw > 0 {
			return 58 * (1 + 0.01*math.Sin(2*math.Pi*7*t)), 11.3
		}
		return kw, khz
	}
	return build("B-2026-0003", baseProgram(), pr)
}

// emissivity 测温发射率设错：仪器设定 0.90，工件真实 0.35（氧化皮），
// 读数系统性偏离能量平衡。
type emissivity struct{}

func (emissivity) Name() string { return "emissivity" }
func (emissivity) Expect() []domain.AnnotationType {
	return []domain.AnnotationType{domain.AnnEmissivity}
}
func (emissivity) Generate() domain.BatchData {
	prog := baseProgram()
	prog.EmissivitySet = 0.90 // 操作员误设
	pr := baseProfile()
	pr.emissTrue = 0.35
	return build("B-2026-0004", prog, pr)
}

// quenchDelay 喷液阀延迟：阀后流量确认滞后 850ms（限值 350ms）。
type quenchDelay struct{}

func (quenchDelay) Name() string { return "quench_delay" }
func (quenchDelay) Expect() []domain.AnnotationType {
	return []domain.AnnotationType{domain.AnnQuenchDelay}
}
func (quenchDelay) Generate() domain.BatchData {
	pr := baseProfile()
	pr.quenchLagS = 0.850
	return build("B-2026-0005", baseProgram(), pr)
}

// clockDrift 高频采样时钟漂移：采集卡时基 +2000ppm。
// 对齐引擎应拟合并校正，校正后轨迹无缺口。
type clockDrift struct{}

func (clockDrift) Name() string { return "clock_drift" }
func (clockDrift) Expect() []domain.AnnotationType {
	return []domain.AnnotationType{domain.AnnClockDrift}
}
func (clockDrift) Generate() domain.BatchData {
	pr := baseProfile()
	pr.srcStretch = 1.002 // +2000ppm
	return build("B-2026-0006", baseProgram(), pr)
}
