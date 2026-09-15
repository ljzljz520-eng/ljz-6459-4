// replayd 齿轮感应淬火复盘服务。
//
// 职责：对齐多源信号（ClickHouse 高频轨迹、NATS 机床事件、gRPC 测温摘要），
// 标注能量密度突变与轨迹缺口等候选，供工程师复盘。
//
// 安全边界：本服务只消费数据，绝不向机床发送任何控制指令；
// 代码库中不存在任何指向机床控制通道的客户端。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/gear-hardening-replay/internal/approval"
	"github.com/example/gear-hardening-replay/internal/domain"
	"github.com/example/gear-hardening-replay/internal/httpserver"
	"github.com/example/gear-hardening-replay/internal/ingest"
	chstore "github.com/example/gear-hardening-replay/internal/ingest/clickhouse"
	natsconsumer "github.com/example/gear-hardening-replay/internal/ingest/nats"
	"github.com/example/gear-hardening-replay/internal/ingest/pyrometer"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// 角色目录：生产环境对接 IAM；此处为演示账号。
	roles := map[string]domain.Role{
		"eng1": domain.RoleEngineer,
		"op1":  domain.RoleOperator,
		"qa1":  domain.RoleInspector,
	}
	svc := approval.New(roles)
	annStore := ingest.NewMemAnnotationStore()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 可选适配器：配置存在才启用，缺省为纯演示模式（回放场景驱动）。
	if dsn := os.Getenv("CLICKHOUSE_DSN"); dsn != "" {
		st, err := chstore.Dial(dsn)
		if err != nil {
			log.Fatalf("clickhouse: %v", err)
		}
		defer st.Close()
		log.Printf("clickhouse 已连接: 高频轨迹读写启用")
	}
	if url := os.Getenv("NATS_URL"); url != "" {
		c, err := natsconsumer.Dial(url, os.Getenv("NATS_SUBJECT"))
		if err != nil {
			log.Fatalf("nats: %v", err)
		}
		defer c.Close()
		go func() {
			err := c.Subscribe(ctx, func(ev domain.MachineEvent) error {
				// 事件驱动批次状态推进（只读消费，不回发任何指令）
				batchID := ev.Payload["batch_id"]
				switch ev.Type {
				case domain.EvScanStart:
					_ = svc.MarkRunning(batchID)
				case domain.EvProgramEnd:
					_ = svc.MarkDone(batchID)
				}
				return nil
			})
			if err != nil && ctx.Err() == nil {
				log.Printf("nats 订阅退出: %v", err)
			}
		}()
		log.Printf("nats 已连接: 机床事件消费启用")
	}
	if addr := os.Getenv("PYRO_ADDR"); addr != "" {
		pc, err := pyrometer.Dial(addr)
		if err != nil {
			log.Fatalf("pyrometer: %v", err)
		}
		defer pc.Close()
		_ = pc // 测温摘要在批次复盘时按需拉取
		log.Printf("pyrometer gRPC 已连接: %s", addr)
	}

	srv := &http.Server{
		Addr:              envOr("LISTEN_ADDR", ":8080"),
		Handler:           httpserver.New(svc, annStore).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("复盘服务监听 %s（只读对齐与标注，无控制回路）", srv.Addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("http: %v", err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
