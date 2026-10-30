package grpc

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/backend/kube"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"github.com/tinkerbell/tinkerbell/pkg/journal"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	"github.com/tinkerbell/tinkerbell/tink/server/tunnel"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// defaultExecutionTimeout bounds sessions of a Workflow whose Template sets no global_timeout.
const defaultExecutionTimeout = time.Hour

// Reasons a caller fails to authenticate as an Agent, the reason label values of
// AuthenticationFailures.
const (
	// failedWireGuardKey is a key that isn't the Agent's pinned WireGuard key.
	failedWireGuardKey = "wireguard_key"
	// failedTPMEK is TPM evidence that is missing, invalid, or names another EK.
	failedTPMEK = "tpm_ek"
	// failedTPMChallenge is a wrong answer to a TPM challenge.
	failedTPMChallenge  = "tpm_challenge"
	failedBoundKey      = "bound_key"
	failedEnrollmentKey = "enrollment_key"
	failedSourceCheck   = "source_check"
	// failedNotPinned is a known Agent whose credential isn't pinned, when only pinned credentials are allowed.
	failedNotPinned = "not_pinned"
)

// AuthenticationFailures counts Register calls whose caller failed to
// authenticate as the Agent it named. A rise can mean an attempt to take an
// Agent over, or an Agent that lost its key mid-Workflow and needs a rebind. The
// details are logged at V(2); the labels are fixed sets so the cardinality is too.
var AuthenticationFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "tink_server_agent_authentication_failures_total",
	Help: "Register calls whose caller failed to authenticate as the Agent it named, by credential type, trust, and reason.",
}, []string{"type", "trust", "reason"})

// Tunnel is the WireGuard state Register needs.
type Tunnel interface {
	TicketSealer
	MoveTo(c *tunnel.Caller) (tunnel.BootstrapServer, bool)
	PinnedOnly() bool
	Registered(c *tunnel.Caller)
	Rejected(c *tunnel.Caller)
	NewSession(uid types.UID, src netip.AddrPort) (*tunnel.Grant, error)
	IndexWorkflow(wf *tinkerbell.Workflow)
	SourceCheck() bool
}

// AgentService serves Register inside the bootstrap tunnel.
type AgentService struct {
	h *Handler
	proto.UnimplementedAgentServiceServer
}

// NewAgentService returns the AgentService for h. h.Tunnel must be set.
func NewAgentService(h *Handler) *AgentService {
	return &AgentService{h: h}
}

// Register finds Workflow work for the caller's Agent and, if the caller's
// bootstrap key is the one that Agent is bound to, grants it a session for
// that Workflow. See docs/technical/designs/AGENT_WIREGUARD_AUTHN.md.
func (a *AgentService) Register(ctx context.Context, req *proto.RegisterRequest) (*proto.RegisterResponse, error) {
	h := a.h
	select {
	case <-ctx.Done():
		return nil, status.Error(codes.Unavailable, "server shutting down")
	default:
	}
	c := tunnel.CallerFromContext(ctx)
	if c == nil || c.Tier != tunnel.TierBootstrap || h.Tunnel == nil {
		return nil, status.Error(codes.Unauthenticated, "Register is only served in the WireGuard bootstrap tunnel")
	}
	h.Tunnel.Registered(c)
	// The previous bootstrap key's device only redirects: it grants nothing,
	// whatever the caller's Agent has to do.
	if mv, ok := h.Tunnel.MoveTo(c); ok {
		h.Logger.V(1).Info("Register on the previous bootstrap key, redirecting", "agent", req.GetAgentId(), "bootstrapKey", c.Key.String())
		return &proto.RegisterResponse{
			State:  proto.RegisterResponse_STATE_WAITING.Enum(),
			MoveTo: &proto.BootstrapServer{PublicKey: mv.PublicKey[:], Endpoint: toPtr(mv.Endpoint.String())},
		}, nil
	}

	ctx = journal.New(ctx)
	log := h.Logger.WithValues("agent", req.GetAgentId(), "bootstrapKey", c.Key.String(), "source", c.Source)
	defer func() {
		log.V(1).Info("Register code flow journal", "journal", journal.Journal(ctx))
	}()

	resp, err := a.decide(ctx, log, c, req)
	switch status.Code(err) { //nolint:exhaustive // only refusals end the peer early
	case codes.NotFound, codes.PermissionDenied, codes.InvalidArgument:
		h.Tunnel.Rejected(c)
	}
	if err != nil {
		// Idle Agents poll every few seconds, so only refusals an operator may
		// need to act on are logged by default.
		l := log.V(1)
		if refusalNeedsAttention(err) {
			l = log
		}
		l.Info("Register refused", "code", status.Code(err).String(), "reason", status.Convert(err).Message())
		return nil, err
	}
	if resp.GetState() == proto.RegisterResponse_STATE_GRANTED {
		log.V(1).Info("Register granted a session", "workflow", resp.GetSession().GetWorkflow())
	} else {
		log.V(1).Info("Register", "state", resp.GetState().String())
	}
	return resp, nil
}

// principal is a caller authenticated as an Agent. A tpm caller is only
// fully authenticated once it answers a challenge, which tpm is kept for.
type principal struct {
	agentID string
	typ     tinkerbell.CredentialType
	trust   tinkerbell.CredentialTrust
	key     string
	hw      *tinkerbell.Hardware
	tpm     *tpmIdentity
}

// decide runs Register: authenticate the caller, authorize it for work, finish
// authenticating once there is something to grant, and issue a session.
func (a *AgentService) decide(ctx context.Context, log logr.Logger, c *tunnel.Caller, req *proto.RegisterRequest) (*proto.RegisterResponse, error) {
	h := a.h
	attrs := convert(req.GetAgentAttributes())
	p, err := a.authenticate(ctx, log, c, req, attrs)
	if err != nil {
		return nil, err
	}

	wf, wfs, err := h.workflowWithWork(ctx, p.agentID)
	if err != nil {
		return nil, err
	}
	// Hardware that opts out of enrollment is simply waiting for a Workflow.
	// With only pinned credentials allowed, enrollment can't trust a key on
	// first use, so only Hardware with a pinned credential enrolls.
	enroll := wf == nil && len(wfs) == 0 && h.AutoCapabilities.Enrollment.Enabled &&
		(p.hw == nil || p.hw.Spec.Auto.EnrollmentEnabled) &&
		(!h.Tunnel.PinnedOnly() || p.trust == tinkerbell.CredentialTrustPinned)
	switch {
	case wf == nil && !enroll && p.hw == nil:
		return nil, status.Errorf(codes.NotFound, "unknown Agent %s", p.agentID)
	case wf == nil && !enroll:
		resp := waiting()
		if why := endedSessionReason(wfs, p.agentID, p.key, h.NowFunc()); why != "" {
			resp.Reason = toPtr(why)
		}
		return resp, nil
	case wf != nil && p.hw == nil:
		// Every Agent's identity is a Hardware object; a Workflow alone doesn't make one.
		return nil, status.Errorf(codes.NotFound, "Agent %s has no Hardware", p.agentID)
	}

	// A TPM is challenged only when there is something to grant or enroll, so
	// idle polls cost no TPM operations.
	if p.tpm != nil {
		if resp, err := a.authenticateTPM(ctx, log, c, req, p.tpm, p.agentID); resp != nil || err != nil {
			return resp, err
		}
	}

	if wf == nil {
		journal.Log(ctx, "auto enrollment triggered")
		created, err := h.enrollKey(ctx, p.agentID, attrs, p.hw, p.key)
		if err != nil {
			return nil, err
		}
		if !created && p.hw == nil {
			return nil, status.Errorf(codes.NotFound, "unknown Agent %s", p.agentID)
		}
		return waiting(), nil
	}
	return a.grant(ctx, log, c, types.NamespacedName{Namespace: wf.Namespace, Name: wf.Name}, p)
}

// authenticate finds the caller's identity, the Hardware with its Agent ID, and
// checks what its credential allows without a round trip. It reads
// no Workflows: they only authorize.
func (a *AgentService) authenticate(ctx context.Context, log logr.Logger, c *tunnel.Caller, req *proto.RegisterRequest, attrs *data.AgentAttributes) (*principal, error) {
	h := a.h
	p := &principal{agentID: req.GetAgentId(), key: c.Key.String()}
	if p.agentID == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid Agent ID")
	}
	hw, err := h.hardware(ctx, p.agentID)
	if err != nil {
		return nil, hardwareLookupError(ctx, log, p.agentID, err)
	}
	// Discovery would create Hardware for any Agent ID the caller names, and
	// with only pinned credentials allowed, nothing would pin this one.
	if hw == nil && h.AutoCapabilities.Discovery.Enabled && !h.Tunnel.PinnedOnly() {
		journal.Log(ctx, "auto discovery triggered")
		if hw, err = h.Discover(ctx, p.agentID, attrs); err != nil {
			log.Error(err, "error auto discovering Hardware")
			return nil, errors.Join(ErrBackendWrite, status.Errorf(codes.Unavailable, "error auto discovering Hardware: %v", err))
		}
	}
	p.hw = hw

	var auth *tinkerbell.AgentAuthentication
	if hw != nil {
		auth = hw.Spec.Agent.Authentication
	}
	p.typ, p.trust = auth.Credential()
	journal.Log(ctx, "credential", "type", p.typ, "trust", p.trust)

	if p.trust == tinkerbell.CredentialTrustFirstUse && h.Tunnel.PinnedOnly() && hw != nil {
		authnFailed(log, p.typ, p.trust, failedNotPinned)
		return nil, status.Errorf(codes.PermissionDenied,
			"Agent %s has no pinned credential and only pinned credentials are allowed: pin one in spec.agent.authentication on its Hardware", p.agentID)
	}
	if p.tpm, err = checkPinned(log, p.agentID, p.key, auth, req.GetEvidence()); err != nil {
		return nil, err
	}
	return p, nil
}

// checkPinned refuses a caller that a pinned credential rules out without a
// challenge, so a misconfigured Agent learns it while idle. For tpm, it returns
// the caller's TPM, which must still answer a challenge.
func checkPinned(log logr.Logger, agentID, key string, auth *tinkerbell.AgentAuthentication, ev *proto.Evidence) (*tpmIdentity, error) {
	typ, trust := auth.Credential()
	if trust != tinkerbell.CredentialTrustPinned {
		return nil, nil
	}
	switch typ {
	case tinkerbell.CredentialTypeWireGuard:
		if auth.WireGuard.PublicKey != key {
			authnFailed(log, typ, trust, failedWireGuardKey, "acceptedKey", auth.WireGuard.PublicKey)
			return nil, status.Errorf(codes.PermissionDenied, "Agent %s only accepts its pinned WireGuard key", agentID)
		}
	case tinkerbell.CredentialTypeTPM:
		id, err := parseTPMEvidence(ev)
		if err != nil {
			authnFailed(log, typ, trust, failedTPMEK, "error", err.Error())
			return nil, status.Errorf(codes.PermissionDenied, "Agent %s authenticates with its TPM: %v", agentID, err)
		}
		if id.ekHash != auth.TPM.EKPublicKeyHash {
			authnFailed(log, typ, trust, failedTPMEK, "ekPublicKeyHash", id.ekHash, "acceptedEKPublicKeyHash", auth.TPM.EKPublicKeyHash)
			return nil, status.Errorf(codes.PermissionDenied, "Agent %s only accepts the TPM with EK public key hash %s; this TPM's is %s", agentID, auth.TPM.EKPublicKeyHash, id.ekHash)
		}
		return id, nil
	}
	return nil, nil
}

// authenticateTPM proves that the caller's TPM holds the pinned EK. It returns
// a CHALLENGE response until the caller answers one, and nil, nil once it has.
func (a *AgentService) authenticateTPM(ctx context.Context, log logr.Logger, c *tunnel.Caller, req *proto.RegisterRequest, id *tpmIdentity, agentID string) (*proto.RegisterResponse, error) {
	h := a.h
	key := c.Key.String()
	now := h.NowFunc()
	if r := req.GetChallengeResponse(); r != nil {
		err := checkTPMAnswer(h.Tunnel, r, id, agentID, key, now)
		if err == nil {
			journal.Log(ctx, "TPM challenge answered")
			return nil, nil
		}
		if !errors.Is(err, errTicketExpired) {
			authnFailed(log, tinkerbell.CredentialTypeTPM, tinkerbell.CredentialTrustPinned, failedTPMChallenge, "error", err.Error())
			return nil, status.Errorf(codes.PermissionDenied, "Agent %s: TPM challenge failed: %v", agentID, err)
		}
		journal.Log(ctx, "TPM challenge expired, issuing another")
	}
	ch, err := tpmChallenge(h.Tunnel, id, agentID, key, now)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error creating TPM challenge: %v", err)
	}
	journal.Log(ctx, "TPM challenge issued")
	return &proto.RegisterResponse{State: proto.RegisterResponse_STATE_CHALLENGE.Enum(), Challenge: ch}, nil
}

// grant binds or re-issues p's session in the Workflow nn.
func (a *AgentService) grant(ctx context.Context, log logr.Logger, c *tunnel.Caller, nn types.NamespacedName, p *principal) (*proto.RegisterResponse, error) {
	h := a.h
	agentID, key, hw := p.agentID, p.key, p.hw
	// The cache found the Workflow; the write needs its current resourceVersion
	// so that only one replica's binding wins.
	wf, err := h.Backend.ReadWorkflow(ctx, nn.Name, nn.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return waiting(), nil
		}
		return nil, errors.Join(ErrBackendRead, status.Errorf(codes.Unavailable, "error reading Workflow: %v", err))
	}
	if !hasWork(wf, agentID) {
		return waiting(), nil
	}

	rec := wf.Status.Agent(agentID)
	switch {
	case rec != nil && rec.Credential.WireGuard.PublicKey == key:
		journal.Log(ctx, "re-issuing session to the bound key")
	case rec != nil && rec.Credential.WireGuard.PublicKey != "" && p.trust == tinkerbell.CredentialTrustFirstUse:
		journal.Log(ctx, "Agent is bound to another key")
		authnFailed(log, p.typ, p.trust, failedBoundKey, "workflow", nn.String(), "boundKey", rec.Credential.WireGuard.PublicKey)
		return nil, status.Errorf(codes.PermissionDenied, "Agent %s is bound to another key in Workflow %s", agentID, nn)
	case p.trust == tinkerbell.CredentialTrustPinned:
		// The pinned credential authenticated the caller, so the session moves to its key.
		journal.Log(ctx, "pinned credential authenticates the Agent")
	default:
		// The first session binds the caller's key.
		if ek := wf.Annotations[EnrollmentKeyAnnotation]; ek != "" && ek != key {
			authnFailed(log, p.typ, p.trust, failedEnrollmentKey, "workflow", nn.String(), "enrolledKey", ek)
			return nil, status.Errorf(codes.PermissionDenied, "Workflow %s was enrolled by another key", nn)
		}
		// A rebind to an empty key skips the source check, as re-issues do.
		if rec == nil && h.Tunnel.SourceCheck() && hw != nil && !autoDiscovered(hw) {
			if !slices.Contains(kube.GetIPs(hw), c.Source.Addr().String()) {
				journal.Log(ctx, "source check failed")
				authnFailed(log, p.typ, p.trust, failedSourceCheck, "hardware", hw.Name, "hardwareIPs", kube.GetIPs(hw))
				return nil, status.Errorf(codes.PermissionDenied, "source %s is not an address of Hardware %s", c.Source.Addr(), hw.Name)
			}
		}
		journal.Log(ctx, "binding the caller's key")
	}

	// metav1.Time is stored with second precision, so truncate here for the
	// response's expiry to match what every replica and the controller read back.
	now := h.NowFunc().UTC().Truncate(time.Second)
	if wf.Status.GlobalExecutionStart == nil {
		timeout := time.Duration(wf.Status.GlobalTimeout) * time.Second
		if timeout <= 0 {
			timeout = defaultExecutionTimeout
		}
		wf.Status.GlobalExecutionStart = &metav1.Time{Time: now}
		// A stop set earlier, on the plain path, is kept: a grant never extends it.
		if wf.Status.GlobalExecutionStop == nil {
			wf.Status.GlobalExecutionStop = &metav1.Time{Time: now.Add(timeout)}
		}
		journal.Log(ctx, "execution clock started", "deadline", wf.Status.GlobalExecutionStop)
	}
	deadline := wf.Status.GlobalExecutionStop.Time
	if !now.Before(deadline) {
		tunnel.AuthorizationDenials.WithLabelValues(tunnel.DeniedDeadlinePassed).Inc()
		return nil, status.Errorf(codes.FailedPrecondition, "Workflow %s passed its execution deadline", nn)
	}

	g, err := h.Tunnel.NewSession(wf.UID, c.Source)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error generating session key: %v", err)
	}
	newRec := tinkerbell.WorkflowAgentStatus{
		AgentID: agentID,
		Credential: tinkerbell.AgentCredential{
			Type:            p.typ,
			Trust:           p.trust,
			WireGuard:       tinkerbell.WireGuardCredential{PublicKey: key, Endpoint: c.Source.String()},
			AuthenticatedAt: &metav1.Time{Time: now},
		},
		Session: tinkerbell.AgentSession{
			PublicKey: g.PublicKey.String(),
			Address:   g.Address.String(),
			IssuedAt:  &metav1.Time{Time: now},
			ExpiresAt: &metav1.Time{Time: deadline},
		},
	}
	if p.tpm != nil {
		newRec.Credential.TPM = &tinkerbell.TPMCredential{EKPublicKeyHash: p.tpm.ekHash}
	}
	if rec != nil {
		*rec = newRec
	} else {
		wf.Status.Agents = append(wf.Status.Agents, newRec)
	}
	if err := h.Backend.UpdateWorkflow(ctx, wf, data.UpdateOptions{StatusOnly: true}); err != nil {
		if apierrors.IsConflict(err) {
			return nil, status.Errorf(codes.Aborted, "error writing session: %v", err)
		}
		return nil, errors.Join(ErrBackendWrite, status.Errorf(codes.Internal, "error writing session: %v", err))
	}
	// Index now so this replica admits the Agent's handshake without waiting for its cache.
	h.Tunnel.IndexWorkflow(wf)

	return &proto.RegisterResponse{
		State: proto.RegisterResponse_STATE_GRANTED.Enum(),
		Session: &proto.Session{
			Workflow:        toPtr(nn.String()),
			PrivateKey:      g.PrivateKey[:],
			PresharedKey:    g.PresharedKey[:],
			Address:         toPtr(g.Address.String()),
			ServerPublicKey: g.ServerPublicKey[:],
			ServerEndpoint:  toPtr(g.ServerEndpoint.String()),
			ServerAddress:   toPtr(g.ServerAddress.String()),
			ExpiresAt:       timestamppb.New(deadline),
		},
	}, nil
}

// workflowWithWork returns the Workflow that has work for agentID, if any, and
// every Workflow that references agentID.
func (h *Handler) workflowWithWork(ctx context.Context, agentID string) (*tinkerbell.Workflow, []tinkerbell.Workflow, error) {
	wfs, err := h.Backend.ListWorkflows(ctx, data.WorkflowFilter{ByAgentID: agentID})
	if err != nil {
		return nil, nil, errors.Join(ErrBackendRead, status.Errorf(codes.Internal, "error getting workflows: %v", err))
	}
	for i := range wfs {
		if hasWork(&wfs[i], agentID) {
			return &wfs[i], wfs, nil
		}
	}
	return nil, wfs, nil
}

// endedSessionReason explains why the most recent session key last held for
// agentID in wfs ended, so an Agent that just lost its session can say why.
// It returns "" if key never had a session for agentID.
func endedSessionReason(wfs []tinkerbell.Workflow, agentID, key string, now time.Time) string {
	var reason string
	var latest time.Time
	for i := range wfs {
		rec := wfs[i].Status.Agent(agentID)
		if rec == nil || rec.Credential.WireGuard.PublicKey != key || rec.Session.IssuedAt == nil || rec.Session.IssuedAt.Time.Before(latest) {
			continue
		}
		if r := tunnel.SessionEndReason(&wfs[i], rec, now); r != "" {
			reason, latest = r, rec.Session.IssuedAt.Time
		}
	}
	return reason
}

// hasWork reports whether wf is ready to execute and its current Task, the first
// one that hasn't finished, is assigned to agentID.
func hasWork(wf *tinkerbell.Workflow, agentID string) bool {
	if wf.Status.State != tinkerbell.WorkflowStatePending && wf.Status.State != tinkerbell.WorkflowStateRunning {
		return false
	}
	for _, t := range wf.Status.Tasks {
		if isTaskSuccessful(t) {
			continue
		}
		return t.AgentID == agentID
	}
	return false
}

// refusalNeedsAttention reports whether a Register error is worth logging by
// default: a key refused for an Agent, or the server failing. Routine answers to
// idle or unknown Agents are not.
func refusalNeedsAttention(err error) bool {
	switch status.Code(err) { //nolint:exhaustive // every other code is a routine answer
	case codes.PermissionDenied, codes.Internal, codes.Unavailable, codes.Unknown, codes.Unauthenticated:
		return true
	}
	return false
}

func autoDiscovered(hw *tinkerbell.Hardware) bool {
	return hw.Labels["tinkerbell.org/auto-discovered"] == "true"
}

func waiting() *proto.RegisterResponse {
	return &proto.RegisterResponse{State: proto.RegisterResponse_STATE_WAITING.Enum()}
}

// authnFailed counts and logs a caller that failed to authenticate as an Agent.
// The log carries the caller's key and source (already on log) for an operator
// who needs them, for example to rebind; it is at V(2) because a refused Agent
// retries every poll.
func authnFailed(log logr.Logger, typ tinkerbell.CredentialType, trust tinkerbell.CredentialTrust, reason string, kv ...any) {
	AuthenticationFailures.WithLabelValues(string(typ), string(trust), reason).Inc()
	log.V(2).Info("caller failed to authenticate as the Agent", append([]any{"credentialType", typ, "trust", trust, "reason", reason}, kv...)...)
}
