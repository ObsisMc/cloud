// Package controlgrpc serves the Controller-facing internal control contract (internal/controlpb)
// over gRPC. It is a translation layer only: every RPC verifies the caller's service credential,
// converts the request into a core.ControlRequest, runs it through the same Store.Control
// transaction as the JSON internal API, and maps the resulting Fault to a gRPC status. No business
// rule, cache or retry lives here.
package controlgrpc

import (
	"context"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/core"
)

// Role is the only service role admitted to the control contract; Nodes keep their HTTP surface.
const Role = "controller"

type claimsKey struct{}

// New builds the gRPC server with credential verification on every unary and stream call. The
// caller owns the listener and the stop sequence.
func New(store *core.Store, auth *core.Authenticator, log *zap.Logger) *grpc.Server {
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(unaryAuth(auth, log)), grpc.ChainStreamInterceptor(streamAuth(auth, log)))
	controlpb.RegisterControllerLeaseServiceServer(server, &leaseService{store: store})
	controlpb.RegisterExecutionServiceServer(server, &executionService{store: store})
	controlpb.RegisterControlSignalServiceServer(server, &signalService{store: store})
	return server
}

// verify admits exactly one verified controller service principal from the authorization metadata.
// Credentials are never logged; only the outcome is.
func verify(ctx context.Context, auth *core.Authenticator, log *zap.Logger, method string) (context.Context, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return nil, status.Error(codes.Unauthenticated, "service credential required")
	}
	claims, e := auth.Verify(strings.TrimPrefix(values[0], "Bearer "), "service")
	if e != nil {
		log.Warn("control credential rejected", zap.String("method", method))
		return nil, status.Error(codes.Unauthenticated, "invalid service credential")
	}
	if claims.Role != Role {
		return nil, status.Error(codes.PermissionDenied, "service_forbidden")
	}
	return context.WithValue(ctx, claimsKey{}, claims), nil
}

func unaryAuth(auth *core.Authenticator, log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, e := verify(ctx, auth, log, info.FullMethod)
		if e != nil {
			return nil, e
		}
		return handler(ctx, req)
	}
}

func streamAuth(auth *core.Authenticator, log *zap.Logger) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, e := verify(stream.Context(), auth, log, info.FullMethod)
		if e != nil {
			return e
		}
		return handler(srv, &authenticatedStream{ServerStream: stream, ctx: ctx})
	}
}

// authenticatedStream carries the verified principal to stream handlers through Context().
type authenticatedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authenticatedStream) Context() context.Context { return s.ctx }

// principal returns the claims the interceptor verified; handlers are only reachable through it.
func principal(ctx context.Context) *core.Claims {
	claims, _ := ctx.Value(claimsKey{}).(*core.Claims)
	return claims
}
