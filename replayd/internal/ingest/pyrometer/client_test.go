package pyrometer

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// fakeServer 进程内假测温服务。
type fakeServer struct{}

func (fakeServer) GetSummary(_ context.Context, req *SummaryRequest) (*SummaryReply, error) {
	return &SummaryReply{Points: []struct {
		Ts         string  `json:"ts"`
		Channel    string  `json:"channel"`
		TempC      float64 `json:"temp_c"`
		Emissivity float64 `json:"emissivity"`
	}{
		{Ts: "2026-09-15T08:00:05.5Z", Channel: "IR-1", TempC: 812.3, Emissivity: 0.6},
		{Ts: "2026-09-15T08:00:06.5Z", Channel: "IR-1", TempC: 845.1, Emissivity: 0.6},
	}}, nil
}

var serviceDesc = grpc.ServiceDesc{
	ServiceName: "pyrometer.v1.PyrometerService",
	HandlerType: (*interface {
		GetSummary(context.Context, *SummaryRequest) (*SummaryReply, error)
	})(nil),
	Methods: []grpc.MethodDesc{{
		MethodName: "GetSummary",
		Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			req := new(SummaryRequest)
			if err := dec(req); err != nil {
				return nil, err
			}
			return srv.(interface {
				GetSummary(context.Context, *SummaryRequest) (*SummaryReply, error)
			}).GetSummary(ctx, req)
		},
	}},
}

func TestGetSummaryOverBufconn(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	srv.RegisterService(&serviceDesc, fakeServer{})
	go srv.Serve(lis)
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(jsonCodec{})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var reply SummaryReply
	if err := conn.Invoke(context.Background(),
		"/pyrometer.v1.PyrometerService/GetSummary",
		&SummaryRequest{BatchId: "B-2026-0001"}, &reply); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(reply.Points) != 2 {
		t.Fatalf("期望 2 个测温点，实际 %d", len(reply.Points))
	}
	ts, err := time.Parse(time.RFC3339Nano, reply.Points[0].Ts)
	if err != nil || ts.Hour() != 8 {
		t.Errorf("时间解析失败: %v %v", ts, err)
	}
	if reply.Points[1].TempC != 845.1 {
		t.Errorf("第二点温度应为 845.1，实际 %.1f", reply.Points[1].TempC)
	}
}
