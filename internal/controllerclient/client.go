// Package controllerclient dials one Controller's gRPC contract (internal/controllerpb) and turns
// its status details into typed errors. It carries no business rules: callers decide what a clone
// means for a tenant; this package only speaks the Controller's language.
package controllerclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/wanglongan587/cloud/internal/controllerpb"
)

// Client wraps one connection; per-call deadlines keep a stalled Controller from holding a request.
type Client struct {
	conn    *grpc.ClientConn
	rpc     controllerpb.ControllerServiceClient
	timeout time.Duration
}

// Error is a classified Controller failure. Code comes from the ErrorDetail the Controller attached;
// it is UNSPECIFIED when the response carried only a status code, which Status still reports.
type Error struct {
	Code    controllerpb.ErrorCode
	Status  codes.Code
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("controller %s (%s): %s", e.Code, e.Status, e.Message)
}

// Target converts the deployment address forms the Controller listens on into a grpc-go target.
// "unix:///absolute/path" is passed through; "tcp://host:port" becomes a plain host:port target.
func Target(address string) (string, error) {
	switch {
	case strings.HasPrefix(address, "unix://"):
		if !strings.HasPrefix(address, "unix:///") {
			return "", fmt.Errorf("controller address %q: unix socket path must be absolute", address)
		}
		return address, nil
	case strings.HasPrefix(address, "tcp://"):
		host := strings.TrimPrefix(address, "tcp://")
		if host == "" || strings.ContainsAny(host, "/?#") {
			return "", fmt.Errorf("controller address %q: expected tcp://host:port", address)
		}
		return host, nil
	default:
		return "", fmt.Errorf("controller address %q: expected tcp://host:port or unix:///path", address)
	}
}

// Dial opens a plaintext connection (h2c on TCP, h2 on a Unix socket), matching the Controller's
// unauthenticated loopback and socket transports; credentials arrive with the authentication ADR.
func Dial(address string, timeout time.Duration) (*Client, error) {
	target, err := Target(address)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial controller %s: %w", address, err)
	}
	return New(conn, timeout), nil
}

// New adopts an existing connection; tests use it with an in-memory listener.
func New(conn *grpc.ClientConn, timeout time.Duration) *Client {
	return &Client{conn: conn, rpc: controllerpb.NewControllerServiceClient(conn), timeout: timeout}
}

// Close releases the connection; it does not cancel anything the Controller already accepted.
func (c *Client) Close() error {
	return c.conn.Close()
}

// AcceptClone submits durable clone intent. Repeating requestID with the same input returns the
// original receipt; different input yields an Error with Code CONFLICT.
func (c *Client) AcceptClone(ctx context.Context, requestID, repository, branch string) (*controllerpb.AcceptCloneResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	out, err := c.rpc.AcceptClone(ctx, &controllerpb.AcceptCloneRequest{RequestId: requestID, Repository: repository, Branch: branch})
	if err != nil {
		return nil, classify(err)
	}
	return out, nil
}

// ListOperations returns accepted operations newest first, including pending ones while the Node is offline.
func (c *Client) ListOperations(ctx context.Context) ([]*controllerpb.CloneOperation, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	out, err := c.rpc.ListOperations(ctx, &controllerpb.ListOperationsRequest{})
	if err != nil {
		return nil, classify(err)
	}
	return out.GetOperations(), nil
}

// GetOperation reads one operation; an unknown execution is an Error with Code NOT_FOUND, which is
// distinct from an accepted operation that merely has no terminal state yet.
func (c *Client) GetOperation(ctx context.Context, executionID string) (*controllerpb.CloneOperation, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	out, err := c.rpc.GetOperation(ctx, &controllerpb.GetOperationRequest{ExecutionId: executionID})
	if err != nil {
		return nil, classify(err)
	}
	return out.GetOperation(), nil
}

// classify turns Controller statuses into Error, preferring the attached ErrorDetail over the status
// code so both sides branch on the enum. Deadline and cancellation are the caller's own outcomes,
// not Controller verdicts, so they pass through untouched for errors.Is/status.Code inspection.
func classify(err error) error {
	st, ok := status.FromError(err)
	if !ok || st.Code() == codes.DeadlineExceeded || st.Code() == codes.Canceled {
		return err
	}
	out := &Error{Code: controllerpb.ErrorCode_ERROR_CODE_UNSPECIFIED, Status: st.Code(), Message: st.Message()}
	for _, detail := range st.Details() {
		if typed, isDetail := detail.(*controllerpb.ErrorDetail); isDetail {
			out.Code = typed.GetCode()
		}
	}
	return out
}
