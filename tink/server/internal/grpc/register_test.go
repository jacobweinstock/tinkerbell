package grpc

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
	"github.com/tinkerbell/tinkerbell/tink/server/tunnel"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

const testAgent = "aa:bb:cc:dd:ee:01"

// registerBackend is an in-memory backend with at most one Hardware and one Workflow.
type registerBackend struct {
	hw *tinkerbell.Hardware
	wf *tinkerbell.Workflow
}

func (b *registerBackend) ReadWorkflow(_ context.Context, _, _ string) (*tinkerbell.Workflow, error) {
	if b.wf == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "workflows"}, "")
	}
	return b.wf.DeepCopy(), nil
}

func (b *registerBackend) ListWorkflows(_ context.Context, f data.WorkflowFilter) ([]tinkerbell.Workflow, error) {
	if b.wf == nil || (f.ByAgentID != "" && b.wf.Status.AgentID != f.ByAgentID) {
		return nil, nil
	}
	return []tinkerbell.Workflow{*b.wf.DeepCopy()}, nil
}

func (b *registerBackend) UpdateWorkflow(_ context.Context, wf *tinkerbell.Workflow, _ data.UpdateOptions) error {
	b.wf = wf.DeepCopy()
	return nil
}

func (b *registerBackend) ReadHardware(_ context.Context, _, _ string) (*tinkerbell.Hardware, error) {
	return b.hw, nil
}

func (b *registerBackend) FilterHardware(_ context.Context, _ data.HardwareFilter) (*tinkerbell.Hardware, error) {
	if b.hw == nil {
		return nil, notFoundError{}
	}
	return b.hw.DeepCopy(), nil
}

func (b *registerBackend) UpdateHardware(_ context.Context, hw *tinkerbell.Hardware, _ data.UpdateOptions) error {
	b.hw = hw.DeepCopy()
	return nil
}

func (b *registerBackend) ApplyHardwareInBandAttributes(context.Context, string, string, *tinkerbell.Attributes) error {
	return nil
}

type fakeTunnel struct {
	pinned   bool
	previous bool
	// tickets stands in for sealed tickets; tunnel's tests cover the sealing.
	tickets  map[string]tunnel.Ticket
	rejected int
	indexed  int
}

func (f *fakeTunnel) MoveTo(*tunnel.Caller) (tunnel.BootstrapServer, bool) {
	k, _ := wireguard.GeneratePrivateKey()
	return tunnel.BootstrapServer{PublicKey: k.PublicKey(), Endpoint: netip.MustParseAddrPort("192.0.2.1:51822")}, f.previous
}

func (f *fakeTunnel) PinnedOnly() bool          { return f.pinned }
func (f *fakeTunnel) Registered(*tunnel.Caller) {}
func (f *fakeTunnel) Rejected(*tunnel.Caller)   { f.rejected++ }
func (f *fakeTunnel) NewSession(types.UID, netip.AddrPort) (*tunnel.Grant, error) {
	priv, _ := wireguard.GeneratePrivateKey()
	return &tunnel.Grant{
		PrivateKey:     priv,
		PublicKey:      priv.PublicKey(),
		Address:        netip.MustParseAddr("fd00:0:0:1::2"),
		ServerEndpoint: netip.MustParseAddrPort("192.0.2.1:51822"),
		ServerAddress:  netip.MustParseAddr("fd00:0:0:1::1"),
	}, nil
}
func (f *fakeTunnel) IndexWorkflow(*tinkerbell.Workflow) { f.indexed++ }

func (f *fakeTunnel) SealTicket(t tunnel.Ticket) ([]byte, error) {
	if f.tickets == nil {
		f.tickets = map[string]tunnel.Ticket{}
	}
	id := fmt.Sprintf("ticket-%d", len(f.tickets))
	f.tickets[id] = t
	return []byte(id), nil
}

func (f *fakeTunnel) OpenTicket(b []byte) (tunnel.Ticket, error) {
	t, ok := f.tickets[string(b)]
	if !ok {
		return tunnel.Ticket{}, errors.New("unknown ticket")
	}
	return t, nil
}
func (f *fakeTunnel) SourceCheck() bool { return false }

func pendingWorkflow() *tinkerbell.Workflow {
	return &tinkerbell.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "wf", Namespace: "default", UID: "uid"},
		Status: tinkerbell.WorkflowStatus{
			AgentID:       testAgent,
			State:         tinkerbell.WorkflowStatePending,
			GlobalTimeout: 600,
			Tasks: []tinkerbell.Task{{
				AgentID: testAgent,
				Actions: []tinkerbell.Action{{ID: "a1", State: tinkerbell.WorkflowStatePending}},
			}},
		},
	}
}

// hardware returns the test Agent's Hardware, pinning key as its WireGuard key
// unless key is empty.
func hardware(key string) *tinkerbell.Hardware {
	hw := &tinkerbell.Hardware{
		ObjectMeta: metav1.ObjectMeta{Name: "hw", Namespace: "default"},
		Spec:       tinkerbell.HardwareSpec{AgentID: testAgent},
	}
	if key != "" {
		hw.Spec.Agent.Authentication = &tinkerbell.AgentAuthentication{WireGuard: &tinkerbell.WireGuardAuthentication{PublicKey: key}}
	}
	return hw
}

func newKey(t *testing.T) wireguard.Key {
	t.Helper()
	k, err := wireguard.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k.PublicKey()
}

func register(t *testing.T, b *registerBackend, tun *fakeTunnel, key wireguard.Key, now time.Time) (*proto.RegisterResponse, error) {
	t.Helper()
	return registerWith(t, &Handler{Backend: b}, tun, key, now)
}

func registerWith(t *testing.T, h *Handler, tun *fakeTunnel, key wireguard.Key, now time.Time) (*proto.RegisterResponse, error) {
	t.Helper()
	return registerReq(t, h, tun, key, now, &proto.RegisterRequest{AgentId: toPtr(testAgent)})
}

func registerReq(t *testing.T, h *Handler, tun *fakeTunnel, key wireguard.Key, now time.Time, req *proto.RegisterRequest) (*proto.RegisterResponse, error) {
	t.Helper()
	h.Logger, h.NowFunc, h.Tunnel = logr.Discard(), func() time.Time { return now }, tun
	ctx := tunnel.WithCaller(context.Background(), &tunnel.Caller{
		Tier: tunnel.TierBootstrap, Key: key, Source: netip.MustParseAddrPort("192.0.2.10:41641"),
	})
	return NewAgentService(h).Register(ctx, req)
}

// failures returns how many callers failed to authenticate with method for reason.
func failures(typ tinkerbell.CredentialType, trust tinkerbell.CredentialTrust, reason string) float64 {
	return testutil.ToFloat64(AuthenticationFailures.WithLabelValues(string(typ), string(trust), reason))
}

const (
	wg       = tinkerbell.CredentialTypeWireGuard
	firstUse = tinkerbell.CredentialTrustFirstUse
	pinned   = tinkerbell.CredentialTrustPinned
)

func TestRegisterBindsFirstKeyAndRefusesOthers(t *testing.T) {
	now := time.Date(2026, 10, 22, 20, 10, 0, 775_000_000, time.UTC)
	b := &registerBackend{hw: hardware(""), wf: pendingWorkflow()}
	tun := &fakeTunnel{}
	first, second := newKey(t), newKey(t)

	resp, err := register(t, b, tun, first, now)
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if resp.GetState() != proto.RegisterResponse_STATE_GRANTED || resp.GetSession().GetWorkflow() != "default/wf" {
		t.Fatalf("first Register = %v", resp)
	}
	rec := b.wf.Status.Agent(testAgent)
	if rec == nil || rec.Credential.WireGuard.PublicKey != first.String() || rec.Session.PublicKey == "" {
		t.Fatalf("session record = %+v", rec)
	}
	if got, want := b.wf.Status.GlobalExecutionStop.Time, now.Truncate(time.Second).Add(600*time.Second); !got.Equal(want) {
		t.Errorf("deadline = %v, want %v", got, want)
	}
	if got := resp.GetSession().GetExpiresAt().AsTime(); !got.Equal(b.wf.Status.GlobalExecutionStop.Time) {
		t.Errorf("response expiry %v != stored deadline %v", got, b.wf.Status.GlobalExecutionStop.Time)
	}
	if !rec.Session.ExpiresAt.Time.Equal(b.wf.Status.GlobalExecutionStop.Time) {
		t.Errorf("session expiry %v != deadline %v", rec.Session.ExpiresAt, b.wf.Status.GlobalExecutionStop)
	}
	firstSession := rec.Session.PublicKey

	// The bound key is re-issued a new session, and the deadline doesn't move.
	if _, err := register(t, b, tun, first, now.Add(time.Minute)); err != nil {
		t.Fatalf("re-issue: %v", err)
	}
	rec = b.wf.Status.Agent(testAgent)
	if rec.Session.PublicKey == firstSession {
		t.Error("re-issue kept the old session key")
	}
	if got, want := rec.Session.ExpiresAt.Time, now.Truncate(time.Second).Add(600*time.Second); !got.Equal(want) {
		t.Errorf("re-issue moved the expiry to %v, want %v", got, want)
	}

	// Another key is refused and counted, without writing to the Workflow.
	before, rv := failures(wg, firstUse, failedBoundKey), b.wf.DeepCopy()
	_, err = register(t, b, tun, second, now.Add(time.Minute))
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("other key: %v, want PermissionDenied", err)
	}
	if failures(wg, firstUse, failedBoundKey) != before+1 {
		t.Error("bound-key refusal not counted")
	}
	if !equality.Semantic.DeepEqual(rv, b.wf) {
		t.Error("refusal changed the Workflow")
	}
	if tun.rejected != 1 {
		t.Errorf("rejected = %d, want 1", tun.rejected)
	}
	if b.wf.Status.Agent(testAgent).Credential.WireGuard.PublicKey != first.String() {
		t.Error("binding changed")
	}
}

func TestRegisterPinnedWireGuardKey(t *testing.T) {
	now := time.Now()
	persistent, other := newKey(t), newKey(t)
	b := &registerBackend{hw: hardware(persistent.String()), wf: pendingWorkflow()}
	tun := &fakeTunnel{}

	before := failures(wg, pinned, failedWireGuardKey)
	if _, err := register(t, b, tun, other, now); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("other key: %v, want PermissionDenied", err)
	}
	if failures(wg, pinned, failedWireGuardKey) != before+1 {
		t.Error("pinned-key refusal not counted")
	}
	if b.wf.Status.Agent(testAgent) != nil {
		t.Error("other key got a session record")
	}
	resp, err := register(t, b, tun, persistent, now)
	if err != nil || resp.GetState() != proto.RegisterResponse_STATE_GRANTED {
		t.Fatalf("pinned key: %v, %v", resp, err)
	}
	if c := b.wf.Status.Agent(testAgent).Credential; c.Type != wg || c.Trust != pinned || c.TPM != nil || c.AuthenticatedAt == nil {
		t.Errorf("credential = %+v, want a pinned WireGuard key", c)
	}
}

// TestRegisterWorkflowIsNoIdentity checks that a Workflow naming an Agent
// doesn't make it one: every Agent's identity is a Hardware object.
func TestRegisterWorkflowIsNoIdentity(t *testing.T) {
	b := &registerBackend{wf: pendingWorkflow()}
	if _, err := register(t, b, &fakeTunnel{}, newKey(t), time.Now()); status.Code(err) != codes.NotFound {
		t.Fatalf("Register: %v, want NotFound", err)
	}
	if b.wf.Status.Agent(testAgent) != nil {
		t.Error("an Agent without Hardware got a session")
	}
}

func TestRegisterWaitingAndUnknown(t *testing.T) {
	now := time.Now()
	resp, err := register(t, &registerBackend{hw: hardware("")}, &fakeTunnel{}, newKey(t), now)
	if err != nil || resp.GetState() != proto.RegisterResponse_STATE_WAITING {
		t.Errorf("known Agent without work: %v, %v", resp, err)
	}
	tun := &fakeTunnel{}
	if _, err := register(t, &registerBackend{}, tun, newKey(t), now); status.Code(err) != codes.NotFound {
		t.Errorf("unknown Agent: %v, want NotFound", err)
	}
	if tun.rejected != 1 {
		t.Errorf("rejected = %d, want 1", tun.rejected)
	}
}

func TestRegisterDeadlinePassed(t *testing.T) {
	now := time.Now()
	wf := pendingWorkflow()
	wf.Status.GlobalExecutionStart = &metav1.Time{Time: now.Add(-time.Hour)}
	wf.Status.GlobalExecutionStop = &metav1.Time{Time: now.Add(-time.Second)}
	b := &registerBackend{hw: hardware(""), wf: wf}
	denials := func() float64 {
		return testutil.ToFloat64(tunnel.AuthorizationDenials.WithLabelValues(tunnel.DeniedDeadlinePassed))
	}
	before := denials()
	if _, err := register(t, b, &fakeTunnel{}, newKey(t), now); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("Register: %v, want FailedPrecondition", err)
	}
	if denials() != before+1 {
		t.Error("authorization denial not counted")
	}
}

func TestRegisterOtherAgentsTaskFirst(t *testing.T) {
	wf := pendingWorkflow()
	wf.Status.Tasks = append([]tinkerbell.Task{{
		AgentID: "admin",
		Actions: []tinkerbell.Action{{ID: "x", State: tinkerbell.WorkflowStatePending}},
	}}, wf.Status.Tasks...)
	b := &registerBackend{hw: hardware(""), wf: wf}
	resp, err := register(t, b, &fakeTunnel{}, newKey(t), time.Now())
	if err != nil || resp.GetState() != proto.RegisterResponse_STATE_WAITING {
		t.Errorf("Register = %v, %v; want WAITING until the Agent's own Task is current", resp, err)
	}
}

func TestRegisterEnrollmentDisabledHardwareWaits(t *testing.T) {
	// Enrollment is on, but this Hardware opts out, so it waits instead of being refused.
	h := &Handler{
		Backend:          &registerBackend{hw: hardware("")},
		AutoCapabilities: AutoCapabilities{Enrollment: AutoEnrollment{Enabled: true}},
	}
	resp, err := registerWith(t, h, &fakeTunnel{}, newKey(t), time.Now())
	if err != nil || resp.GetState() != proto.RegisterResponse_STATE_WAITING {
		t.Errorf("Register = %v, %v; want WAITING", resp, err)
	}
}

func TestRegisterWaitingSaysWhyTheSessionEnded(t *testing.T) {
	now := time.Now()
	b := &registerBackend{hw: hardware(""), wf: pendingWorkflow()}
	key := newKey(t)
	if _, err := register(t, b, &fakeTunnel{}, key, now); err != nil {
		t.Fatal(err)
	}
	b.wf.Status.State = tinkerbell.WorkflowStateTimeout

	resp, err := register(t, b, &fakeTunnel{}, key, now)
	if err != nil || resp.GetState() != proto.RegisterResponse_STATE_WAITING {
		t.Fatalf("Register = %v, %v; want WAITING", resp, err)
	}
	if got, want := resp.GetReason(), "Workflow default/wf is TIMEOUT"; got != want {
		t.Errorf("reason = %q, want %q", got, want)
	}
	// Another key learns nothing about this key's Workflows, and no empty
	// reason is put on the wire.
	if resp, _ := register(t, b, &fakeTunnel{}, newKey(t), now); resp.Reason != nil {
		t.Errorf("other key got reason %q", resp.GetReason())
	}
}

func TestRegisterPreviousBootstrapKeyRedirects(t *testing.T) {
	b := &registerBackend{hw: hardware(""), wf: pendingWorkflow()}
	resp, err := register(t, b, &fakeTunnel{previous: true}, newKey(t), time.Now())
	if err != nil || resp.GetState() != proto.RegisterResponse_STATE_WAITING || resp.GetMoveTo().GetEndpoint() != "192.0.2.1:51822" {
		t.Fatalf("Register = %v, %v; want WAITING with move_to", resp, err)
	}
	if b.wf.Status.Agent(testAgent) != nil {
		t.Error("the previous bootstrap key's device granted a session")
	}
}

func TestRegisterPinnedPolicy(t *testing.T) {
	now := time.Now()
	strict := func() *fakeTunnel { return &fakeTunnel{pinned: true} }

	t.Run("Hardware key is granted", func(t *testing.T) {
		key := newKey(t)
		b := &registerBackend{hw: hardware(key.String()), wf: pendingWorkflow()}
		if resp, err := register(t, b, strict(), key, now); err != nil || resp.GetState() != proto.RegisterResponse_STATE_GRANTED {
			t.Fatalf("Register = %v, %v; want GRANTED", resp, err)
		}
	})

	// The bootstrap device answers every key, so Register refuses an Agent
	// without a pinned credential of its own.
	for name, b := range map[string]*registerBackend{
		"keyless Hardware with work":    {hw: hardware(""), wf: pendingWorkflow()},
		"keyless Hardware without work": {hw: hardware("")},
	} {
		t.Run(name+" is refused", func(t *testing.T) {
			tun := strict()
			before := failures(wg, firstUse, failedNotPinned)
			if _, err := register(t, b, tun, newKey(t), now); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("Register: %v, want PermissionDenied", err)
			}
			if failures(wg, firstUse, failedNotPinned) != before+1 {
				t.Error("refusal not counted")
			}
			if tun.rejected != 1 {
				t.Errorf("rejected = %d, want 1", tun.rejected)
			}
			if b.wf != nil && b.wf.Status.Agent(testAgent) != nil {
				t.Error("refused key got a session record")
			}
		})
	}

	t.Run("unknown Agent is neither discovered nor enrolled", func(t *testing.T) {
		h := &Handler{
			Backend: &registerBackend{},
			AutoCapabilities: AutoCapabilities{
				Discovery:  AutoDiscovery{Enabled: true},
				Enrollment: AutoEnrollment{Enabled: true},
			},
		}
		if _, err := registerWith(t, h, strict(), newKey(t), now); status.Code(err) != codes.NotFound {
			t.Fatalf("Register: %v, want NotFound", err)
		}
	})
}

// TestRegisterEnrollmentCreatesHardware checks that auto enrollment gives an
// Agent without Hardware an identity first, in the matched rule set's
// namespace, and owns the Workflow by the enrolling key.
func TestRegisterEnrollmentCreatesHardware(t *testing.T) {
	var hw *tinkerbell.Hardware
	var wf *tinkerbell.Workflow
	caps := &mockAutoCapabilities{
		ListWorkflowRuleSetsFunc: func(context.Context, data.WorkflowFilter) ([]tinkerbell.WorkflowRuleSet, error) {
			return []tinkerbell.WorkflowRuleSet{{
				ObjectMeta: metav1.ObjectMeta{Name: "rs", Namespace: "tink"},
				Spec:       tinkerbell.WorkflowRuleSetSpec{Rules: []string{`{"chassis": {"serial": ["12345"]}}`}},
			}}, nil
		},
		CreateWorkflowFunc: func(_ context.Context, w *tinkerbell.Workflow) error {
			wf = w
			return nil
		},
	}
	b := &registerBackend{}
	h := &Handler{
		Backend: b,
		AutoCapabilities: AutoCapabilities{
			Enrollment: AutoEnrollment{Enabled: true, WorkflowRuleSetLister: caps, WorkflowCreator: caps},
			// Discovery is off: enrollment creates the Hardware itself.
			Discovery: AutoDiscovery{
				HardwareFilterer: b,
				HardwareCreator: createHardwareFunc(func(_ context.Context, h *tinkerbell.Hardware) error {
					hw = h
					return nil
				}),
			},
		},
	}
	key := newKey(t)
	req := &proto.RegisterRequest{
		AgentId:         toPtr(testAgent),
		AgentAttributes: &proto.AgentAttributes{Chassis: &proto.Chassis{Serial: toPtr("12345")}},
	}
	resp, err := registerReq(t, h, &fakeTunnel{}, key, time.Now(), req)
	if err != nil || resp.GetState() != proto.RegisterResponse_STATE_WAITING {
		t.Fatalf("Register = %v, %v; want WAITING", resp, err)
	}
	if hw == nil || hw.Namespace != "tink" || hw.Spec.AgentID != testAgent || hw.Spec.Agent.Authentication != nil {
		t.Fatalf("created Hardware = %+v; want one in tink for the Agent, without a pinned credential", hw)
	}
	if wf == nil || wf.Spec.HardwareRef != hw.Name || wf.Annotations[EnrollmentKeyAnnotation] != key.String() {
		t.Fatalf("created Workflow = %+v; want it on the new Hardware, owned by the enrolling key", wf)
	}
}

// TestRegisterWireGuardWithoutKeyIsFirstUse checks that naming the WireGuard
// credential type without a key trusts the key on first use, as no
// authentication does.
func TestRegisterWireGuardWithoutKeyIsFirstUse(t *testing.T) {
	hw := hardware("")
	hw.Spec.Agent.Authentication = &tinkerbell.AgentAuthentication{WireGuard: &tinkerbell.WireGuardAuthentication{}}
	b := &registerBackend{hw: hw, wf: pendingWorkflow()}
	key := newKey(t)
	if resp, err := register(t, b, &fakeTunnel{}, key, time.Now()); err != nil || resp.GetState() != proto.RegisterResponse_STATE_GRANTED {
		t.Fatalf("Register = %v, %v; want GRANTED", resp, err)
	}
	if c := b.wf.Status.Agent(testAgent).Credential; c.Type != wg || c.Trust != firstUse || c.WireGuard.PublicKey != key.String() {
		t.Errorf("credential = %+v, want the caller's key trusted on first use", c)
	}
	if _, err := register(t, b, &fakeTunnel{}, newKey(t), time.Now()); status.Code(err) != codes.PermissionDenied {
		t.Errorf("another key: %v, want PermissionDenied", err)
	}
}

// TestRegisterKeepsEarlierStop checks that the first grant starts the
// execution clock but never extends a stop time set earlier.
func TestRegisterKeepsEarlierStop(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	wf := pendingWorkflow()
	stop := now.Add(time.Minute)
	wf.Status.GlobalExecutionStop = &metav1.Time{Time: stop}
	b := &registerBackend{hw: hardware(""), wf: wf}
	resp, err := register(t, b, &fakeTunnel{}, newKey(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if b.wf.Status.GlobalExecutionStart == nil || !b.wf.Status.GlobalExecutionStop.Time.Equal(stop) {
		t.Errorf("start %v, stop %v; want the clock started and the stop kept at %v", b.wf.Status.GlobalExecutionStart, b.wf.Status.GlobalExecutionStop, stop)
	}
	if got := resp.GetSession().GetExpiresAt().AsTime(); !got.Equal(stop) {
		t.Errorf("session expiry %v, want %v", got, stop)
	}
}
