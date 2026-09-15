// Package ingest 定义外部系统适配器端口（六边形架构）。
// 核心引擎不依赖任何具体中间件；适配器可独立替换与测试。
//
// 数据流向（全部单向流入，服务无任何控制回路）：
//
//	机床事件  ──NATS──▶ EventSource ──▶ 对齐/标注
//	高频功率/轴位置 ──▶ TraceStore(ClickHouse) ──▶ 查询回放
//	红外测温摘要 ◀──gRPC── PyrometerClient
package ingest

import (
	"context"

	"github.com/example/gear-hardening-replay/internal/domain"
)

// EventSource 机床事件订阅（NATS 适配器实现）。
type EventSource interface {
	// Subscribe 订阅事件流，handler 逐条处理；ctx 取消后返回。
	Subscribe(ctx context.Context, handler func(domain.MachineEvent) error) error
	Close() error
}

// TraceStore 高频轨迹存取（ClickHouse 适配器实现）。
type TraceStore interface {
	InsertPower(ctx context.Context, batchID string, samples []domain.PowerSample) error
	InsertAxis(ctx context.Context, batchID string, samples []domain.AxisSample) error
	QueryPower(ctx context.Context, batchID string) ([]domain.PowerSample, error)
	QueryAxis(ctx context.Context, batchID string) ([]domain.AxisSample, error)
	Close() error
}

// PyrometerClient 红外测温摘要客户端（gRPC 适配器实现）。
type PyrometerClient interface {
	// GetSummary 拉取批次时间窗内的测温摘要。
	GetSummary(ctx context.Context, batchID string) ([]domain.PyroSummary, error)
}

// AnnotationStore 标注结果存取。
type AnnotationStore interface {
	Put(ctx context.Context, anns []domain.Annotation) error
	ByBatch(ctx context.Context, batchID string) ([]domain.Annotation, error)
}

// MemAnnotationStore 内存标注存储（演示与测试用）。
type MemAnnotationStore struct {
	m map[string][]domain.Annotation
}

func NewMemAnnotationStore() *MemAnnotationStore {
	return &MemAnnotationStore{m: map[string][]domain.Annotation{}}
}

func (s *MemAnnotationStore) Put(_ context.Context, anns []domain.Annotation) error {
	for _, a := range anns {
		s.m[a.BatchID] = append(s.m[a.BatchID], a)
	}
	return nil
}

func (s *MemAnnotationStore) ByBatch(_ context.Context, batchID string) ([]domain.Annotation, error) {
	return s.m[batchID], nil
}
