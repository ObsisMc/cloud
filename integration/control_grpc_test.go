package integration

import (
	"context"
	"net"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/wanglongan587/cloud/internal/controlgrpc"
	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/core"
	"github.com/wanglongan587/cloud/internal/simulator"
)

// The gRPC control surface admits only controller service credentials and runs the same lease
// transaction as the JSON internal API: one holder, monotonic epochs, stale epochs fenced.
func TestControlGRPCLeaseAuthenticationAndFencing(t *testing.T) {
	pool, _ := testSchema(t, "grpc_")
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	must(t, e)
	store, e := core.NewStore(db)
	must(t, e)
	must(t, store.Migrate(context.Background()))
	credentials, e := simulator.NewCredentials()
	must(t, e)
	auth, e := core.NewAuthenticator("ora-cloud", credentials.Trust)
	must(t, e)
	listener := bufconn.Listen(1 << 20)
	server := controlgrpc.New(store, auth, zap.NewNop())
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, e := grpc.NewClient("passthrough:///control", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	must(t, e)
	t.Cleanup(func() { _ = conn.Close() })
	client := controlpb.NewControllerLeaseServiceClient(conn)
	as := func(role, subject string) context.Context {
		token, err := credentials.Token(role, core.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: subject}})
		must(t, err)
		return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token)
	}
	expect := func(err error, code codes.Code, detail controlpb.ErrorCode) {
		t.Helper()
		st := status.Convert(err)
		if st.Code() != code {
			t.Fatalf("status %s (%s), want %s", st.Code(), st.Message(), code)
		}
		got := controlpb.ErrorCode_ERROR_CODE_UNSPECIFIED
		for _, d := range st.Details() {
			if typed, ok := d.(*controlpb.ErrorDetail); ok {
				got = typed.GetCode()
			}
		}
		if got != detail {
			t.Fatalf("detail %s, want %s", got, detail)
		}
	}

	_, e = client.AcquireLease(context.Background(), &controlpb.AcquireLeaseRequest{})
	expect(e, codes.Unauthenticated, controlpb.ErrorCode_ERROR_CODE_UNSPECIFIED)
	_, e = client.AcquireLease(as("gateway", "gateway-a"), &controlpb.AcquireLeaseRequest{})
	expect(e, codes.PermissionDenied, controlpb.ErrorCode_ERROR_CODE_UNSPECIFIED)

	first, e := client.AcquireLease(as("controller", "controller-a"), &controlpb.AcquireLeaseRequest{})
	must(t, e)
	if first.GetLease().GetHolderId() != "controller-a" || first.GetLease().GetEpoch() != 1 || first.GetLease().GetExpiresAt() == nil {
		t.Fatalf("unexpected first lease: %v", first.GetLease())
	}
	_, e = client.AcquireLease(as("controller", "controller-b"), &controlpb.AcquireLeaseRequest{})
	expect(e, codes.FailedPrecondition, controlpb.ErrorCode_ERROR_CODE_LEASE_HELD)
	_, e = client.RenewLease(as("controller", "controller-b"), &controlpb.RenewLeaseRequest{Epoch: 1})
	expect(e, codes.FailedPrecondition, controlpb.ErrorCode_ERROR_CODE_STALE_CONTROLLER)
	_, e = client.RenewLease(as("controller", "controller-a"), &controlpb.RenewLeaseRequest{Epoch: 7})
	expect(e, codes.FailedPrecondition, controlpb.ErrorCode_ERROR_CODE_STALE_CONTROLLER)
	renewed, e := client.RenewLease(as("controller", "controller-a"), &controlpb.RenewLeaseRequest{Epoch: 1})
	must(t, e)
	if renewed.GetLease().GetEpoch() != 1 || !renewed.GetLease().GetExpiresAt().AsTime().After(first.GetLease().GetExpiresAt().AsTime()) {
		t.Fatalf("renewal must keep the epoch and extend expiry: %v -> %v", first.GetLease(), renewed.GetLease())
	}
	released, e := client.ReleaseLease(as("controller", "controller-a"), &controlpb.ReleaseLeaseRequest{Epoch: 1})
	must(t, e)
	if released.GetLease().GetEpoch() != 1 {
		t.Fatalf("release changed the epoch: %v", released.GetLease())
	}
	second, e := client.AcquireLease(as("controller", "controller-b"), &controlpb.AcquireLeaseRequest{})
	must(t, e)
	if second.GetLease().GetHolderId() != "controller-b" || second.GetLease().GetEpoch() != 2 {
		t.Fatalf("hand-over must bump the epoch: %v", second.GetLease())
	}
	_, e = client.RenewLease(as("controller", "controller-a"), &controlpb.RenewLeaseRequest{Epoch: 1})
	expect(e, codes.FailedPrecondition, controlpb.ErrorCode_ERROR_CODE_STALE_CONTROLLER)
}
