package tunnel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

const conflictRetries = 5

// WorkflowHandler keeps the session index in line with every Workflow the
// informer sees, so that every replica removes ended keys. It also applies the
// rebind annotation.
func (m *Manager) WorkflowHandler() cache.ResourceEventHandler {
	onRebind := func(wf *tinkerbell.Workflow) {
		if _, ok := wf.Annotations[tinkerbell.AgentRebindAnnotation]; !ok {
			return
		}
		nn := types.NamespacedName{Namespace: wf.Namespace, Name: wf.Name}
		m.enqueue(func(ctx context.Context) { m.rebind(ctx, nn) })
	}
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if wf, ok := obj.(*tinkerbell.Workflow); ok {
				m.IndexWorkflow(wf)
				onRebind(wf)
			}
		},
		UpdateFunc: func(_, newObj any) {
			if wf, ok := newObj.(*tinkerbell.Workflow); ok {
				m.IndexWorkflow(wf)
				onRebind(wf)
			}
		},
		DeleteFunc: func(obj any) {
			if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				obj = d.Obj
			}
			if wf, ok := obj.(*tinkerbell.Workflow); ok {
				m.ForgetWorkflow(types.NamespacedName{Namespace: wf.Namespace, Name: wf.Name})
			}
		},
	}
}

// ParseRebind parses the rebind annotation's value, "<agentID>=<key>". An
// empty key clears the binding so the next key to Register is bound.
func ParseRebind(v string) (agentID, key string, err error) {
	// Cut on the first "=": base64 keys end in "=".
	agentID, key, ok := strings.Cut(v, "=")
	if !ok || agentID == "" {
		return "", "", fmt.Errorf("want <agentID>=<key>, got %q", v)
	}
	if key != "" {
		if _, err := wireguard.ParseKey(key); err != nil {
			return "", "", fmt.Errorf("invalid key %q: %w", key, err)
		}
	}
	return agentID, key, nil
}

func (m *Manager) rebind(ctx context.Context, nn types.NamespacedName) {
	wf, err := m.cfg.Backend.ReadWorkflow(ctx, nn.Name, nn.Namespace)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			m.log.Error(err, "rebind: reading Workflow", "workflow", nn)
		}
		return
	}
	v, ok := wf.Annotations[tinkerbell.AgentRebindAnnotation]
	if !ok {
		return
	}
	agentID, key, err := ParseRebind(v)
	var trust tinkerbell.CredentialTrust
	if rec := wf.Status.Agent(agentID); err == nil && rec != nil {
		trust = rec.Credential.Trust
	}
	switch {
	case m.PinnedOnly():
		m.log.Info("rebind: ignoring annotation, the Agent authentication policy allows only pinned credentials; change spec.agent.authentication instead", "workflow", nn)
	case trust == tinkerbell.CredentialTrustPinned:
		m.log.Info("rebind: ignoring annotation, the Agent's credential is pinned; change spec.agent.authentication instead", "workflow", nn, "agentID", agentID)
	case err != nil:
		m.log.Error(err, "rebind: ignoring invalid annotation", "workflow", nn)
	default:
		m.handOver(ctx, nn, agentID, key)
		m.log.Info("Agent rebound", "workflow", nn, "agentID", agentID, "key", key)
	}
	m.removeRebindAnnotation(ctx, nn)
}

func (m *Manager) removeRebindAnnotation(ctx context.Context, nn types.NamespacedName) {
	// Re-read: handing over wrote the status, which changed the resourceVersion.
	wf, err := m.cfg.Backend.ReadWorkflow(ctx, nn.Name, nn.Namespace)
	if err != nil {
		return
	}
	orig := wf.DeepCopy()
	delete(wf.Annotations, tinkerbell.AgentRebindAnnotation)
	if err := m.cfg.Backend.UpdateWorkflow(ctx, wf, data.UpdateOptions{PatchFrom: orig}); err != nil {
		m.log.Error(err, "rebind: removing annotation", "workflow", nn)
	}
}

// handOver moves agentID's session record in the Workflow nn to key and
// removes its session key. The next Register from key gets a new session.
func (m *Manager) handOver(ctx context.Context, nn types.NamespacedName, agentID, key string) {
	for range conflictRetries {
		wf, err := m.cfg.Backend.ReadWorkflow(ctx, nn.Name, nn.Namespace)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				m.log.Error(err, "hand over: reading Workflow", "workflow", nn)
			}
			return
		}
		if wf.Status.State != tinkerbell.WorkflowStatePending && wf.Status.State != tinkerbell.WorkflowStateRunning {
			return
		}
		rec := wf.Status.Agent(agentID)
		if rec == nil || (rec.Credential.WireGuard.PublicKey == key && rec.Session.PublicKey == "") {
			return
		}
		rec.Credential.WireGuard.PublicKey = key
		rec.Credential.WireGuard.Endpoint = ""
		if rec.Session.PublicKey != "" {
			rec.Session.PublicKey = ""
			rec.Session.RevokedAt = &metav1.Time{Time: m.now()}
		}
		err = m.cfg.Backend.UpdateWorkflow(ctx, wf, data.UpdateOptions{StatusOnly: true})
		if apierrors.IsConflict(err) {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if err != nil {
			m.log.Error(err, "hand over: updating Workflow", "workflow", nn)
			return
		}
		m.IndexWorkflow(wf)
		m.log.Info("Agent handed over", "workflow", nn, "agentID", agentID, "key", key)
		return
	}
}
