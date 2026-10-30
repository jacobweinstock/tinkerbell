package tunnel

import (
	"context"
	"strings"

	"github.com/tinkerbell/tinkerbell/pkg/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// PlainGuard decides whether a request on the plain listener may proceed. In
// optional mode it refuses Agents that have a pinned credential or a binding.
type PlainGuard func(ctx context.Context, req any) error

// UnaryInterceptor authorizes every request by where it arrived:
//
//   - Register only through the bootstrap tunnel, for any admitted key.
//   - GetAction and ReportActionStatus through the session tunnel, only for the
//     session's Agent and Workflow.
//   - The WorkflowService on the plain listener, subject to guard.
func (m *Manager) UnaryInterceptor(guard PlainGuard) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		svc := service(info.FullMethod)
		var addr *Addr
		if p, ok := peer.FromContext(ctx); ok {
			addr, _ = p.Addr.(*Addr)
		}

		if addr == nil {
			if svc == proto.AgentService_ServiceDesc.ServiceName {
				return nil, status.Error(codes.PermissionDenied, "Register is only served in the WireGuard bootstrap tunnel")
			}
			if m.cfg.Mode == ModeRequired {
				return nil, status.Error(codes.Unauthenticated, "WireGuard is required")
			}
			if guard != nil && m.cfg.Mode == ModeOptional {
				if err := guard(ctx, req); err != nil {
					return nil, err
				}
			}
			return handler(ctx, req)
		}

		switch addr.Tier {
		case TierBootstrap:
			if svc != proto.AgentService_ServiceDesc.ServiceName {
				return nil, status.Error(codes.PermissionDenied, "the bootstrap tunnel only serves Register")
			}
			c, ok := m.bootstrapCaller(addr)
			if !ok {
				return nil, status.Error(codes.Unauthenticated, "unknown bootstrap peer")
			}
			return handler(WithCaller(ctx, c), req)
		case TierSession:
			if svc != proto.WorkflowService_ServiceDesc.ServiceName {
				denied(DeniedWrongService)
				return nil, status.Error(codes.PermissionDenied, "the session tunnel only serves the WorkflowService")
			}
			c, ok := m.sessionCaller(addr)
			if !ok {
				if reason := m.endedReason(addr); reason != "" {
					denied(DeniedSessionEnded)
					return nil, status.Errorf(codes.Unauthenticated, "session ended: %s", reason)
				}
				denied(DeniedNoSession)
				return nil, status.Error(codes.Unauthenticated, "no active session for this key")
			}
			if id := agentIDOf(req); id != c.Session.AgentID {
				denied(DeniedWrongAgent)
				return nil, status.Errorf(codes.PermissionDenied, "session is for Agent %q, not %q", c.Session.AgentID, id)
			}
			if r, ok := req.(*proto.ActionStatusRequest); ok && r.GetWorkflowId() != c.Session.Workflow.String() {
				denied(DeniedWrongWorkflow)
				return nil, status.Errorf(codes.PermissionDenied, "session is for Workflow %s, not %s", c.Session.Workflow, r.GetWorkflowId())
			}
			return handler(WithCaller(ctx, c), req)
		}
		return nil, status.Error(codes.PermissionDenied, "unknown tunnel")
	}
}

// StreamInterceptor refuses every stream from inside a tunnel. The tunnels
// serve only the unary Agent RPCs; streams such as reflection stay on the
// plain listener.
func (m *Manager) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if p, ok := peer.FromContext(ss.Context()); ok {
			if _, ok := p.Addr.(*Addr); ok {
				return status.Error(codes.PermissionDenied, "streams are not served in the WireGuard tunnels")
			}
		}
		return handler(srv, ss)
	}
}

func service(fullMethod string) string {
	s := strings.TrimPrefix(fullMethod, "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[:i]
	}
	return s
}

func agentIDOf(req any) string {
	switch r := req.(type) {
	case *proto.ActionRequest:
		return r.GetAgentId()
	case *proto.ActionStatusRequest:
		return r.GetAgentId()
	case *proto.RegisterRequest:
		return r.GetAgentId()
	}
	return ""
}
