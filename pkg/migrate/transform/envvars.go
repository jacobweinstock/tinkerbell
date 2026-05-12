package transform

import (
	"sort"

	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"
)

// mapToEnvVars converts a v1 environment map into a v2 EnvVar slice
// sorted by key so that the result is stable.
func mapToEnvVars(in map[string]string) []v2.EnvVar {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]v2.EnvVar, 0, len(keys))
	for _, k := range keys {
		out = append(out, v2.EnvVar{Key: k, Value: in[k]})
	}
	return out
}
