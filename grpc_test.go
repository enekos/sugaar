package sugaar_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/eneko/sugaar"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

// TestGRPCServerServesHealthReflection registers the standard health service
// on the sugaar gRPC server and dials it through bufconn. Verifies that
// EnableGRPC produced a working *grpc.Server wired up to the lifecycle.
func TestGRPCServerServesHealth(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	g := app.EnableGRPC(":0") // addr unused with our custom listener
	healthpb.RegisterHealthServer(g.Server, health.NewServer())

	lis := bufconn.Listen(1 << 16)
	go g.Server.Serve(lis)
	defer g.Server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.NewClient("passthrough:bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := healthpb.NewHealthClient(conn)
	resp, err := client.Check(ctx, &healthpb.HealthCheckRequest{Service: ""})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v", resp.Status)
	}
}
