package tinkerbell

import "testing"

func TestAgentAuthenticationCredential(t *testing.T) {
	for name, tt := range map[string]struct {
		auth      *AgentAuthentication
		wantType  CredentialType
		wantTrust CredentialTrust
	}{
		"none":              {nil, CredentialTypeWireGuard, CredentialTrustFirstUse},
		"wireguard, no key": {&AgentAuthentication{WireGuard: &WireGuardAuthentication{}}, CredentialTypeWireGuard, CredentialTrustFirstUse},
		"wireguard key": {
			&AgentAuthentication{WireGuard: &WireGuardAuthentication{PublicKey: "k"}},
			CredentialTypeWireGuard, CredentialTrustPinned,
		},
		"tpm": {
			&AgentAuthentication{TPM: &TPMAuthentication{EKPublicKeyHash: "h"}},
			CredentialTypeTPM, CredentialTrustPinned,
		},
	} {
		typ, trust := tt.auth.Credential()
		if typ != tt.wantType || trust != tt.wantTrust {
			t.Errorf("%s: Credential() = %s, %s; want %s, %s", name, typ, trust, tt.wantType, tt.wantTrust)
		}
	}
}
