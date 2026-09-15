// Package pipeline 组装复盘管线：锚点提取 → 时钟拟合 → 重采样对齐 → 标注。
// 管线为纯函数，输入一批次信号，输出标注候选；无任何对外写操作。
package pipeline

import (
	"github.com/example/gear-hardening-replay/internal/align"
	"github.com/example/gear-hardening-replay/internal/annotate"
	"github.com/example/gear-hardening-replay/internal/domain"
)

// Analyze 对一批次数据执行完整复盘。
// dt 为对齐网格步长（秒），高频淬火数据建议 1~5ms。
func Analyze(data domain.BatchData, eng annotate.Engine, dt float64) (annotate.Input, []domain.Annotation) {
	epoch := data.Epoch
	pairs := align.Anchors(data.Events, data.Power, epoch, epoch, data.Program.PowerSetKW)
	cm := align.FitClock(pairs)
	g := align.Resample(data.Power, data.Axis, cm, epoch, dt)
	in := annotate.Input{
		BatchID:  data.BatchID,
		Epoch:    epoch,
		Prog:     data.Program,
		G:        g,
		Events:   data.Events,
		Pyro:     data.Pyro,
		DriftPPM: cm.DriftPPM(),
	}
	return in, eng.Run(in)
}
