// Package nats 真实 NATS 消息字段接线测试。
// 线上消息：subject machine.events.{机床号}.{事件类型}，
// JSON 顶层字段 ts/batch_id/source，payload 只放事件参数。
package nats

import (
	"testing"
	"time"

	"github.com/example/gear-hardening-replay/internal/domain"
)

func TestToEventWiresRealFields(t *testing.T) {
	// 与线上 homing_done 消息同构的 JSON
	data := []byte(`{
		"ts": "2026-09-15T08:00:02Z",
		"batch_id": "B-2026-0007",
		"source": "cnc",
		"payload": {"reported_z_mm": "4.200"}
	}`)
	ev, err := toEvent("machine.events.M-17.homing_done", data)
	if err != nil {
		t.Fatalf("toEvent: %v", err)
	}
	// 类型取 subject 末段，而非 JSON 字段
	if ev.Type != domain.EvHomingDone {
		t.Errorf("Type = %q，期望 homing_done", ev.Type)
	}
	// batch_id 必须接到领域事件顶层，不能掉进 payload
	if ev.BatchID != "B-2026-0007" {
		t.Errorf("BatchID = %q，期望 B-2026-0007", ev.BatchID)
	}
	if ev.Source != "cnc" {
		t.Errorf("Source = %q，期望 cnc", ev.Source)
	}
	if ev.Payload["reported_z_mm"] != "4.200" {
		t.Errorf("reported_z_mm = %q，期望 4.200", ev.Payload["reported_z_mm"])
	}
	if _, inPayload := ev.Payload["batch_id"]; inPayload {
		t.Error("batch_id 不应出现在 payload 中")
	}
	wantTs := time.Date(2026, 9, 15, 8, 0, 2, 0, time.UTC)
	if !ev.Ts.Equal(wantTs) {
		t.Errorf("Ts = %v，期望 %v", ev.Ts, wantTs)
	}
}

func TestToEventRejectsBadMessages(t *testing.T) {
	if _, err := toEvent("machine.events.M-1.scan_start", []byte(`{not json`)); err == nil {
		t.Error("非法 JSON 应返回错误")
	}
	if _, err := toEvent("machine.events.M-1.scan_start", []byte(`{"ts":"not-a-time"}`)); err == nil {
		t.Error("非法 ts 应返回错误，避免零时间事件污染对齐")
	}
}
