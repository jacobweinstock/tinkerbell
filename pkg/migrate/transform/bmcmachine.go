package transform

import (
	"fmt"

	v1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"
	v2bmc "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell/bmc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BMCMachine converts a v1alpha1 bmc.Machine into a v1alpha2 BMC.
// The group changes from bmc.tinkerbell.org to tinkerbell.org and the
// kind is renamed Machine -> BMC. v1 Status.Power (a PowerState field)
// is dropped; in v1alpha2 power state is expressed only via Conditions.
func BMCMachine(src *v1.Machine) (*v2.BMC, error) {
	if src == nil {
		return nil, fmt.Errorf("BMCMachine: nil source")
	}

	out := &v2.BMC{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v2.GroupVersion.String(),
			Kind:       "BMC",
		},
		ObjectMeta: cleanObjectMeta(src.ObjectMeta),
		Spec: v2.BMCSpec{
			Connection: convertBMCConnection(src.Spec.Connection),
		},
	}

	for _, c := range src.Status.Conditions {
		out.Status.Conditions = append(out.Status.Conditions, v2bmc.Condition{
			Type:           v2bmc.ConditionType(c.Type),
			Status:         v2bmc.ConditionStatus(c.Status),
			Message:        c.Message,
			LastUpdateTime: c.LastUpdateTime,
		})
	}

	return out, nil
}

func convertBMCConnection(src v1.Connection) v2bmc.Connection {
	out := v2bmc.Connection{
		Host:        src.Host,
		InsecureTLS: src.InsecureTLS,
	}
	if src.AuthSecretRef.Name != "" || src.AuthSecretRef.Namespace != "" {
		out.AuthRef = v2bmc.SimpleReference{
			Name:      src.AuthSecretRef.Name,
			Namespace: src.AuthSecretRef.Namespace,
		}
	}
	if src.ProviderOptions != nil {
		out.ProviderOptions = convertBMCProviderOptions(src.ProviderOptions)
	}
	return out
}

func convertBMCProviderOptions(src *v1.ProviderOptions) *v2bmc.ProviderOptions {
	out := &v2bmc.ProviderOptions{}
	for _, p := range src.PreferredOrder {
		out.PreferredOrder = append(out.PreferredOrder, v2bmc.ProviderName(p))
	}
	if src.IntelAMT != nil {
		out.IntelAMT = &v2bmc.IntelAMTOptions{
			Port:       src.IntelAMT.Port,
			HostScheme: src.IntelAMT.HostScheme,
		}
	}
	if src.IPMITOOL != nil {
		out.IPMITOOL = &v2bmc.IPMITOOLOptions{
			Port:        src.IPMITOOL.Port,
			CipherSuite: src.IPMITOOL.CipherSuite,
		}
	}
	if src.Redfish != nil {
		out.Redfish = &v2bmc.RedfishOptions{
			Port:         src.Redfish.Port,
			UseBasicAuth: src.Redfish.UseBasicAuth,
			SystemName:   src.Redfish.SystemName,
		}
	}
	if src.RPC != nil {
		out.RPC = convertRPCOptions(src.RPC)
	}
	// HomeAssistant is v2-only; no v1 equivalent.
	return out
}

func convertRPCOptions(src *v1.RPCOptions) *v2bmc.RPCOptions {
	out := &v2bmc.RPCOptions{
		ConsumerURL:              src.ConsumerURL,
		LogNotificationsDisabled: src.LogNotificationsDisabled,
	}
	if src.Request != nil {
		out.Request = &v2bmc.RequestOpts{
			HTTPContentType: src.Request.HTTPContentType,
			HTTPMethod:      src.Request.HTTPMethod,
			StaticHeaders:   src.Request.StaticHeaders,
			TimestampFormat: src.Request.TimestampFormat,
			TimestampHeader: src.Request.TimestampHeader,
		}
	}
	if src.Signature != nil {
		out.Signature = &v2bmc.SignatureOpts{
			HeaderName:                 src.Signature.HeaderName,
			AppendAlgoToHeaderDisabled: src.Signature.AppendAlgoToHeaderDisabled,
			IncludedPayloadHeaders:     src.Signature.IncludedPayloadHeaders,
		}
	}
	if src.HMAC != nil {
		out.HMAC = &v2bmc.HMACOpts{
			PrefixSigDisabled: src.HMAC.PrefixSigDisabled,
		}
		if src.HMAC.Secrets != nil {
			out.HMAC.Secrets = make(v2bmc.HMACSecrets, len(src.HMAC.Secrets))
			for alg, refs := range src.HMAC.Secrets {
				converted := make([]v2bmc.SimpleReference, 0, len(refs))
				for _, r := range refs {
					converted = append(converted, v2bmc.SimpleReference{
						Name:      r.Name,
						Namespace: r.Namespace,
					})
				}
				out.HMAC.Secrets[v2bmc.HMACAlgorithm(alg)] = converted
			}
		}
	}
	if src.Experimental != nil {
		out.Experimental = &v2bmc.ExperimentalOpts{
			CustomRequestPayload: src.Experimental.CustomRequestPayload,
			DotPath:              src.Experimental.DotPath,
		}
	}
	return out
}
