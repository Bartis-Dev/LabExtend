package leader

import (
	"context"
	"crypto/subtle"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
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
	return verifyAgentIdentity(ctx)
}

// verifyAgentIdentity binds the claimed node identity to the client
// certificate whenever mTLS is in use.
//
// The "agent-id" metadata key is chosen by the agent, and the token is one
// secret shared by the whole cluster, so on a token-only channel any agent can
// claim any node's ID — taking over that node's registry slot and receiving
// the S3 credentials the leader decrypts for its backups. A per-node client
// certificate is the only thing here that a peer cannot forge, so where one
// exists we require it to cover the ID. Deployments without mTLS keep the old
// behavior and remain vulnerable to impersonation by design; issue per-node
// certs (scripts/gen-mtls.sh) to close it.
func verifyAgentIdentity(ctx context.Context) error {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return nil
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return nil // plaintext or server-only TLS — nothing to bind against
	}
	id := agentIDFromContext(ctx)
	if id == "" {
		return status.Error(codes.Unauthenticated, "missing agent-id")
	}
	cert := tlsInfo.State.PeerCertificates[0]
	if cert.Subject.CommonName == id {
		return nil
	}
	for _, name := range cert.DNSNames {
		if name == id {
			return nil
		}
	}
	return status.Errorf(codes.PermissionDenied,
		"agent-id %q is not covered by the client certificate (CN=%q)", id, cert.Subject.CommonName)
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
