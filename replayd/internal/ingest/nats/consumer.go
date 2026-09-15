// Package nats 机床事件 NATS 适配器。
// 订阅 subject 层级：machine.events.{机床号}.{事件类型}，payload 为 JSON。
// 只消费不发布——复盘系统没有通往机床的写路径。
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/example/gear-hardening-replay/internal/domain"
)

// Consumer NATS 事件消费者。
type Consumer struct {
	conn *nats.Conn
	sub  *nats.Subscription
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
	return &Consumer{conn: nc}, nil
}

// eventDTO 线上 JSON 格式。
type eventDTO struct {
	Ts      string            `json:"ts"` // RFC3339Nano
	BatchID string            `json:"batch_id"`
	Source  string            `json:"source"`
	Payload map[string]string `json:"payload,omitempty"`
}

// Subscribe 实现 ingest.EventSource。事件类型取自 subject 末段。
func (c *Consumer) Subscribe(ctx context.Context, handler func(domain.MachineEvent) error) error {
	sub, err := c.conn.Subscribe("machine.events.>", func(msg *nats.Msg) {
		var dto eventDTO
		if err := json.Unmarshal(msg.Data, &dto); err != nil {
			return // 坏消息丢弃并记录（生产环境进死信队列）
		}
		parts := strings.Split(msg.Subject, ".")
		typ := parts[len(parts)-1]
		ev := domain.MachineEvent{Source: dto.Source, Type: typ, Payload: dto.Payload}
		ev.Ts, _ = time.Parse(time.RFC3339Nano, dto.Ts)
		_ = handler(ev) // handler 错误不阻断消费，由上层决定重试策略
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
