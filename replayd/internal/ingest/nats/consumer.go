// Package nats 机床事件 NATS 适配器。
// 订阅 subject 层级：machine.events.{机床号}.{事件类型}，消息体为 JSON。
// 只消费不发布——复盘系统没有通往机床的写路径。
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/example/gear-hardening-replay/internal/domain"
)

// Consumer NATS 事件消费者。
type Consumer struct {
	conn    *nats.Conn
	sub     *nats.Subscription
	subject string
}

// Dial 连接 NATS 并准备消费。subject 缺省 "machine.events.>"。
func Dial(url, subject string) (*Consumer, error) {
	if subject == "" {
		subject = "machine.events.>"
	}
	nc, err := nats.Connect(url,
		nats.Name("gear-hardening-replay"),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	return &Consumer{conn: nc, subject: subject}, nil
}

// eventDTO 线上 JSON 格式。batch_id 是消息顶层字段，不属于业务 payload。
type eventDTO struct {
	Ts      string            `json:"ts"` // RFC3339Nano
	BatchID string            `json:"batch_id"`
	Source  string            `json:"source"`
	Payload map[string]string `json:"payload,omitempty"`
}

// toEvent 把线上 NATS 消息转成领域事件（纯函数，便于测试）。
// 接线约定：事件类型取 subject 末段（machine.events.{机床号}.{事件类型}），
// batch_id 取消息顶层字段，payload 仅承载事件参数（如 reported_z_mm）。
func toEvent(subject string, data []byte) (domain.MachineEvent, error) {
	var dto eventDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return domain.MachineEvent{}, fmt.Errorf("解码 JSON: %w", err)
	}
	ts, err := time.Parse(time.RFC3339Nano, dto.Ts)
	if err != nil {
		return domain.MachineEvent{}, fmt.Errorf("解析 ts=%q: %w", dto.Ts, err)
	}
	parts := strings.Split(subject, ".")
	typ := parts[len(parts)-1]
	return domain.MachineEvent{
		Ts:      ts,
		BatchID: dto.BatchID,
		Source:  dto.Source,
		Type:    typ,
		Payload: dto.Payload,
	}, nil
}

// Subscribe 实现 ingest.EventSource。
func (c *Consumer) Subscribe(ctx context.Context, handler func(domain.MachineEvent) error) error {
	sub, err := c.conn.Subscribe(c.subject, func(msg *nats.Msg) {
		ev, err := toEvent(msg.Subject, msg.Data)
		if err != nil {
			log.Printf("nats: 丢弃坏消息 subject=%s: %v", msg.Subject, err)
			return // 坏消息丢弃并记录（生产环境进死信队列）
		}
		if err := handler(ev); err != nil {
			// handler 错误不阻断消费，由上层决定重试策略
			log.Printf("nats: 事件处理失败 subject=%s batch=%s type=%s: %v",
				msg.Subject, ev.BatchID, ev.Type, err)
		}
	})
	if err != nil {
		return fmt.Errorf("nats subscribe: %w", err)
	}
	c.sub = sub
	<-ctx.Done()
	return ctx.Err()
}

// Close 退订并关闭连接。
func (c *Consumer) Close() error {
	if c.sub != nil {
		_ = c.sub.Unsubscribe()
	}
	c.conn.Close()
	return nil
}
