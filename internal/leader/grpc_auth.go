package leader

import (
	"context"
	"crypto/subtle"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// authInterceptor enforces shared-secret auth on gRPC calls from agents.
// Agents send the BPM_AGENT_TOKEN value in the "x-agent-token" metadata key.
// We constant-time compare against the leader's configured token.
type authInterceptor struct {
	token string
}

func newAuthInterceptor(token string) *authInterceptor {
	return &authInterceptor{token: token}
}

// validate checks the metadata for a matching x-agent-token.
func (a *authInterceptor) validate(ctx context.Context) error {
	if a.token == "" {
		// Defensive — config.validate() requires this for leader role.
		return status.Errorf(codes.Internal, "leader configured without agent token")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Errorf(codes.Unauthenticated, "missing metadata")
	}
	vals := md.Get("x-agent-token")
	if len(vals) == 0 {
		return status.Errorf(codes.Unauthenticated, "missing x-agent-token")
	}
	if subtle.ConstantTimeCompare([]byte(vals[0]), []byte(a.token)) != 1 {
		return status.Errorf(codes.Unauthenticated, "invalid x-agent-token")
	}
	return nil
}

func (a *authInterceptor) unary(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	if err := a.validate(ctx); err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

func (a *authInterceptor) stream(
	srv any,
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	if err := a.validate(ss.Context()); err != nil {
		return err
	}
	return handler(srv, ss)
}
