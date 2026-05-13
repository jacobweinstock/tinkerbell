package transform

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// cleanObjectMeta returns a copy of ObjectMeta with server-populated
// fields cleared, suitable for re-applying the object to a cluster.
func cleanObjectMeta(in metav1.ObjectMeta) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:        in.Name,
		Namespace:   in.Namespace,
		Labels:      copyMap(in.Labels),
		Annotations: copyMap(in.Annotations),
	}
}

func copyMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
