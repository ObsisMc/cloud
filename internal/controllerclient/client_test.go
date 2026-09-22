package controllerclient

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/wanglongan587/cloud/internal/controllerpb"
)

// stub answers with whatever the test scripted, exercising only the wire and error mapping.
type stub struct {
	controllerpb.UnimplementedControllerServiceServer
	accept func(*controllerpb.AcceptCloneRequest) (*controllerpb.AcceptCloneResponse, error)
	get    func(*controllerpb.GetOperationRequest) (*controllerpb.GetOperationResponse, error)
}

func (s *stub) AcceptClone(_ context.Context, in *controllerpb.AcceptCloneRequest) (*controllerpb.AcceptCloneResponse, error) {
	return s.accept(in)
}

func (s *stub) GetOperation(_ context.Context, in *controllerpb.GetOperationRequest) (*controllerpb.GetOperationResponse, error) {
	return s.get(in)
}

func (s *stub) ListOperations(context.Context, *controllerpb.ListOperationsRequest) (*controllerpb.ListOperationsResponse, error) {
	return &controllerpb.ListOperationsResponse{Operations: []*controllerpb.CloneOperation{{
		OperationId: "op", ExecutionId: "exec", NodeId: "node", Repository: "https://example.com/r.git", Branch: "main",
		State: &controllerpb.CloneOperation_Pending{Pending: &controllerpb.ClonePending{}},
	}}}, nil
}

// serve runs the stub on an in-memory listener and returns a client bound to it.
func serve(t *testing.T, s *stub) *Client {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	controllerpb.RegisterControllerServiceServer(server, s)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///controller",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	client := New(conn, 2*time.Second)
	t.Cleanup(func() {
		_ = client.Close()
		server.Stop()
	})
	return client
}

func TestTargetAcceptsOnlyDeploymentAddressForms(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"tcp://127.0.0.1:4820", "127.0.0.1:4820", true},
		{"unix:///home/ora/controller/api.sock", "unix:///home/ora/controller/api.sock", true},
		{"unix://relative/api.sock", "", false},
		{"tcp://", "", false},
		{"tcp://host:1/path", "", false},
		{"http://127.0.0.1:4820", "", false},
		{"127.0.0.1:4820", "", false},
	}
	for _, c := range cases {
		got, err := Target(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("Target(%q) = %q, %v; want %q, ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

func TestStatusDetailsBecomeTypedErrors(t *testing.T) {
	conflict, err := status.New(codes.Aborted, "identity conflict").WithDetails(&controllerpb.ErrorDetail{Code: controllerpb.ErrorCode_ERROR_CODE_CONFLICT})
	if err != nil {
		t.Fatal(err)
	}
	client := serve(t, &stub{
		accept: func(in *controllerpb.AcceptCloneRequest) (*controllerpb.AcceptCloneResponse, error) {
			if in.GetBranch() == "other" {
				return nil, conflict.Err()
			}
			return &controllerpb.AcceptCloneResponse{RequestId: in.GetRequestId(), OperationId: "op", ExecutionId: "exec"}, nil
		},
		get: func(*controllerpb.GetOperationRequest) (*controllerpb.GetOperationResponse, error) {
			// A Controller status without ErrorDetail still classifies by its code.
			return nil, status.Error(codes.NotFound, "no such execution")
		},
	})
	ctx := context.Background()
	receipt, err := client.AcceptClone(ctx, "r1", "https://example.com/r.git", "main")
	if err != nil || receipt.GetExecutionId() != "exec" || receipt.GetRequestId() != "r1" {
		t.Fatalf("accept: %v %v", receipt, err)
	}
	_, err = client.AcceptClone(ctx, "r1", "https://example.com/r.git", "other")
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != controllerpb.ErrorCode_ERROR_CODE_CONFLICT || typed.Status != codes.Aborted {
		t.Fatalf("conflict: %#v", err)
	}
	_, err = client.GetOperation(ctx, "missing")
	if !errors.As(err, &typed) || typed.Code != controllerpb.ErrorCode_ERROR_CODE_UNSPECIFIED || typed.Status != codes.NotFound {
		t.Fatalf("not found: %#v", err)
	}
	operations, err := client.ListOperations(ctx)
	if err != nil || len(operations) != 1 || operations[0].GetPending() == nil {
		t.Fatalf("list: %v %v", operations, err)
	}
}

func TestDeadlineIsNotReportedAsControllerError(t *testing.T) {
	client := serve(t, &stub{accept: func(*controllerpb.AcceptCloneRequest) (*controllerpb.AcceptCloneResponse, error) {
		time.Sleep(200 * time.Millisecond)
		return &controllerpb.AcceptCloneResponse{}, nil
	}})
	client.timeout = 20 * time.Millisecond
	_, err := client.AcceptClone(context.Background(), "r", "https://example.com/r.git", "main")
	var typed *Error
	if err == nil || errors.As(err, &typed) || status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("deadline: %#v", err)
	}
}
