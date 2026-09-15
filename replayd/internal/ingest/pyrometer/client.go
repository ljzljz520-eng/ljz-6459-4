// Package pyrometer 红外测温摘要 gRPC 客户端。
//
// 契约见 api/pyrometer/v1/pyrometer.proto。为便于无 protoc 环境构建，
// 客户端使用 gRPC 的 JSON 编解码器（content-subtype json），消息结构
// 与 proto 定义一一对应；有 protoc 时可无缝切换为生成代码。
package pyrometer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"

	"github.com/example/gear-hardening-replay/internal/domain"
)

func init() {
	// 注册 JSON 编解码器：消息字段与 proto 定义一一对应。
	encoding.RegisterCodec(jsonCodec{})
}

// jsonCodec 实现 gRPC 自定义编解码（content-subtype: json）。
type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error)   { return json.Marshal(v) }
func (jsonCodec) Unmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
func (jsonCodec) Name() string                    { return "json" }

// SummaryRequest 对应 proto GetSummaryRequest。
type SummaryRequest struct {
	BatchId string `json:"batch_id"`
}

// SummaryReply 对应 proto GetSummaryReply。
type SummaryReply struct {
	Points []struct {
		Ts         string  `json:"ts"` // RFC3339Nano
		Channel    string  `json:"channel"`
		TempC      float64 `json:"temp_c"`
		Emissivity float64 `json:"emissivity"`
	} `json:"points"`
}

// Client 测温摘要客户端。
type Client struct {
	conn *grpc.ClientConn
}

// Dial 连接测温服务。
func Dial(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(jsonCodec{})),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial: %w", err)
	}
	return &Client{conn: conn}, nil
}

// GetSummary 实现 ingest.PyrometerClient。
func (c *Client) GetSummary(ctx context.Context, batchID string) ([]domain.PyroSummary, error) {
	var reply SummaryReply
	err := c.conn.Invoke(ctx, "/pyrometer.v1.PyrometerService/GetSummary",
		&SummaryRequest{BatchId: batchID}, &reply)
	if err != nil {
		return nil, fmt.Errorf("GetSummary: %w", err)
	}
	out := make([]domain.PyroSummary, 0, len(reply.Points))
	for _, p := range reply.Points {
		ts, err := time.Parse(time.RFC3339Nano, p.Ts)
		if err != nil {
			continue
		}
		out = append(out, domain.PyroSummary{
			Ts: ts, Channel: p.Channel, TempC: p.TempC, Emissivity: p.Emissivity,
		})
	}
	return out, nil
}

// Close 关闭连接。
func (c *Client) Close() error { return c.conn.Close() }
