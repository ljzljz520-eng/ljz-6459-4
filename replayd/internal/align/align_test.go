package align

import (
	"math"
	"testing"
	"time"

	"github.com/example/gear-hardening-replay/internal/domain"
)

func TestFitClockIdeal(t *testing.T) {
	m := FitClock([][2]float64{{5, 5}, {25, 25}})
	if math.Abs(m.Alpha-1) > 1e-9 || math.Abs(m.Beta) > 1e-9 {
		t.Errorf("理想时钟应拟合 α=1 β=0，实际 α=%f β=%f", m.Alpha, m.Beta)
	}
}

func TestFitClockDrift(t *testing.T) {
	// 源时钟 +2000ppm：t_src = 1.002·t_ref
	m := FitClock([][2]float64{{5.01, 5.0}, {25.05, 25.0}})
	if ppm := m.DriftPPM(); ppm > -1900 || ppm < -2100 {
		t.Errorf("漂移应约 -2000ppm，实际 %.0f", ppm)
	}
	// 校正后还原参考时间
	if got := m.Apply(25.05); math.Abs(got-25.0) > 0.01 {
		t.Errorf("校正后应为 25.0，实际 %.4f", got)
	}
}

func TestFitClockOffsetOnly(t *testing.T) {
	m := FitClock([][2]float64{{3.5, 5.0}})
	if m.Alpha != 1 || m.Beta != 1.5 {
		t.Errorf("单锚点应退化为偏移校正，实际 α=%f β=%f", m.Alpha, m.Beta)
	}
}

func TestAnchors(t *testing.T) {
	epoch := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	at := func(s float64) time.Time { return epoch.Add(time.Duration(s * float64(time.Second))) }
	pw := []domain.PowerSample{
		{Ts: at(4.9), PowerKW: 0}, {Ts: at(5.01), PowerKW: 80},
		{Ts: at(24.9), PowerKW: 80}, {Ts: at(25.05), PowerKW: 0},
	}
	ev := []domain.MachineEvent{
		{Ts: at(5.0), Type: domain.EvScanStart},
		{Ts: at(25.0), Type: domain.EvScanEnd},
	}
	pairs := Anchors(ev, pw, epoch, epoch, 80)
	if len(pairs) != 2 {
		t.Fatalf("应提取 2 个锚点，实际 %d", len(pairs))
	}
	if pairs[0] != [2]float64{5.01, 5.0} {
		t.Errorf("scan_start 锚点错误: %v", pairs[0])
	}
}

func TestResampleGrid(t *testing.T) {
	epoch := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	at := func(s float64) time.Time { return epoch.Add(time.Duration(s * float64(time.Second))) }
	var pw []domain.PowerSample
	var ax []domain.AxisSample
	for i := 0; i <= 100; i++ {
		t := float64(i) * 0.01
		pw = append(pw, domain.PowerSample{Ts: at(t), PowerKW: 80, FreqKHz: 10})
		ax = append(ax, domain.AxisSample{Ts: at(t), ZMM: 10 + 5*t, ThetaDeg: 0})
	}
	g := Resample(pw, ax, ClockModel{Alpha: 1}, epoch, 0.005)
	if g.N == 0 {
		t.Fatal("网格为空")
	}
	// 中点 z 应≈10+5·0.5=12.5
	mid := g.N / 2
	if z := g.ZMM[mid]; math.Abs(z-(10+5*g.At(mid))) > 0.05 {
		t.Errorf("插值错误: z=%.3f t=%.3f", z, g.At(mid))
	}
}
