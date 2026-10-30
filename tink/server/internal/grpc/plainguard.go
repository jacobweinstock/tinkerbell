package grpc

import (
	"context"
	"strings"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PlainGuard refuses a plain, unauthenticated request for an Agent whose
// Hardware pins a credential, or that has a binding in an active
// Workflow. Such an Agent is reachable only through the tunnels. Agents without
// either keep using the plain path, as before.
func (h *Handler) PlainGuard(ctx context.Context, req any) error {
	var agentID, workflowID string
	switch r := req.(type) {
	case *proto.ActionRequest:
		agentID = r.GetAgentId()
	case *proto.ActionStatusRequest:
		agentID, workflowID = r.GetAgentId(), r.GetWorkflowId()
	default:
		return nil
	}
	if agentID == "" {
		return nil
	}
	denied := status.Errorf(codes.PermissionDenied, "Agent %s must use WireGuard", agentID)

	hw, err := h.hardware(ctx, agentID)
	if err != nil {
		return status.Errorf(codes.Unavailable, "error looking up Hardware: %v", err)
	}
	if hw != nil {
		if _, trust := hw.Spec.Agent.Authentication.Credential(); trust == tinkerbell.CredentialTrustPinned {
			return denied
		}
	}
	wfs, err := h.Backend.ListWorkflows(ctx, data.WorkflowFilter{ByAgentID: agentID})
	if err != nil {
		return status.Errorf(codes.Unavailable, "error listing Workflows: %v", err)
	}
	if ns, name, ok := strings.Cut(workflowID, "/"); ok {
		if wf, err := h.Backend.ReadWorkflow(ctx, name, ns); err == nil {
			wfs = append(wfs, *wf)
		}
	}
	for i := range wfs {
		if claimed(&wfs[i], agentID) {
			return denied
		}
	}
	return nil
}

func claimed(wf *tinkerbell.Workflow, agentID string) bool {
	if wf.Status.State != tinkerbell.WorkflowStatePending && wf.Status.State != tinkerbell.WorkflowStateRunning {
		return false
	}
	rec := wf.Status.Agent(agentID)
	return rec != nil && rec.Credential.WireGuard.PublicKey != ""
}
