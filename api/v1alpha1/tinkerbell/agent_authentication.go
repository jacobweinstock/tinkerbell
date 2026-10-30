package tinkerbell

// CredentialType is the kind of credential that identifies an Agent.
type CredentialType string

const (
	// CredentialTypeWireGuard is the Agent's WireGuard bootstrap key, which the
	// tunnel handshake proves.
	CredentialTypeWireGuard CredentialType = "WireGuard"
	// CredentialTypeTPM is a TPM's endorsement key (EK). The TPM vouches for
	// each boot's WireGuard bootstrap key.
	CredentialTypeTPM CredentialType = "TPM"
)

// CredentialTrust is why an Agent's credential was accepted.
type CredentialTrust string

const (
	// CredentialTrustFirstUse accepts the first credential to Register while a
	// Workflow has work for the Agent, and binds it for that Workflow.
	CredentialTrustFirstUse CredentialTrust = "FirstUse"
	// CredentialTrustPinned accepts only a credential an operator set ahead of time.
	CredentialTrustPinned CredentialTrust = "Pinned"
)

// HardwareAgent holds the settings of a Hardware's Agent. Attestation, the
// appraisal of a machine's state, is reserved for a later field here.
type HardwareAgent struct {
	// Authentication is how the Agent proves it is AgentID, in every Workflow.
	// Without it, the Agent's WireGuard key is trusted on first use.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!(has(self.wireguard) && has(self.tpm))",message="set at most one credential type"
	Authentication *AgentAuthentication `json:"authentication,omitempty"`
}

// AgentAuthentication names the credential type that identifies an Agent and,
// when it is pinned, its value. It is set only on Hardware: every Agent's
// identity is a Hardware object. At most one credential type is set; with
// none, the Agent's WireGuard key is trusted on first use.
type AgentAuthentication struct {
	// WireGuard identifies the Agent by its WireGuard bootstrap key.
	// +optional
	WireGuard *WireGuardAuthentication `json:"wireguard,omitempty"`

	// TPM identifies the Agent by its TPM's endorsement key. The Agent's
	// WireGuard key may change every boot.
	// +optional
	TPM *TPMAuthentication `json:"tpm,omitempty"`
}

// WireGuardAuthentication identifies an Agent by its WireGuard bootstrap key.
type WireGuardAuthentication struct {
	// PublicKey pins the WireGuard bootstrap public key, base64. Without it,
	// the key is trusted on first use.
	// +optional
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9+/]{42}[AEIMQUYcgkosw048]=$`
	PublicKey string `json:"publicKey,omitempty"`
}

// TPMAuthentication identifies an Agent by its TPM's endorsement key (EK).
type TPMAuthentication struct {
	// EKPublicKeyHash pins the EK: the SHA-256 of the EK public key's PKIX DER
	// encoding, lowercase hex. The Agent logs it at startup.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	EKPublicKeyHash string `json:"ekPublicKeyHash"`
}

// Credential returns the credential type a identifies the Agent by, and how
// that credential is trusted. A nil a is a WireGuard key trusted on first use.
func (a *AgentAuthentication) Credential() (CredentialType, CredentialTrust) {
	switch {
	case a != nil && a.TPM != nil:
		return CredentialTypeTPM, CredentialTrustPinned
	case a != nil && a.WireGuard != nil && a.WireGuard.PublicKey != "":
		return CredentialTypeWireGuard, CredentialTrustPinned
	default:
		return CredentialTypeWireGuard, CredentialTrustFirstUse
	}
}
