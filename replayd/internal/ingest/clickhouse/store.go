// Package clickhouse 高频轨迹 ClickHouse 适配器。
// 功率与轴位置按批次分区批量写入；查询用于复盘回放。
package clickhouse

import (
	"context"
	"fmt"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/example/gear-hardening-replay/internal/domain"
)

// Store ClickHouse 轨迹存储。
type Store struct {
	conn driver.Conn
}

// Dial 建立连接。dsn 形如 clickhouse://user:pass@host:9000/hardening。
func Dial(dsn string) (*Store, error) {
	opts, err := ch.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	conn, err := ch.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}
	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}
	return &Store{conn: conn}, nil
}

// InsertPower 批量写入功率采样。
func (s *Store) InsertPower(ctx context.Context, batchID string, samples []domain.PowerSample) error {
	b, err := s.conn.PrepareBatch(ctx, "INSERT INTO hf_power (batch_id, ts, power_kw, freq_khz)")
	if err != nil {
		return err
	}
	for _, p := range samples {
		if err := b.Append(batchID, p.Ts, p.PowerKW, p.FreqKHz); err != nil {
			return err
		}
	}
	return b.Send()
}

// InsertAxis 批量写入轴位置采样。
func (s *Store) InsertAxis(ctx context.Context, batchID string, samples []domain.AxisSample) error {
	b, err := s.conn.PrepareBatch(ctx, "INSERT INTO hf_axis (batch_id, ts, z_mm, theta_deg)")
	if err != nil {
		return err
	}
	for _, a := range samples {
		if err := b.Append(batchID, a.Ts, a.ZMM, a.ThetaDeg); err != nil {
			return err
		}
	}
	return b.Send()
}

// QueryPower 按批次查询功率流（按时间升序）。
func (s *Store) QueryPower(ctx context.Context, batchID string) ([]domain.PowerSample, error) {
	rows, err := s.conn.Query(ctx,
		"SELECT ts, power_kw, freq_khz FROM hf_power WHERE batch_id = ? ORDER BY ts", batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PowerSample
	for rows.Next() {
		var p domain.PowerSample
		if err := rows.Scan(&p.Ts, &p.PowerKW, &p.FreqKHz); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// QueryAxis 按批次查询轴位置流（按时间升序）。
func (s *Store) QueryAxis(ctx context.Context, batchID string) ([]domain.AxisSample, error) {
	rows, err := s.conn.Query(ctx,
		"SELECT ts, z_mm, theta_deg FROM hf_axis WHERE batch_id = ? ORDER BY ts", batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AxisSample
	for rows.Next() {
		var a domain.AxisSample
		if err := rows.Scan(&a.Ts, &a.ZMM, &a.ThetaDeg); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Close 关闭连接。
func (s *Store) Close() error { return s.conn.Close() }
