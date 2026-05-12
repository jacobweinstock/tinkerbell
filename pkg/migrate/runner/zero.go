package runner

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// metav1Zero is a tiny helper that returns a zero metav1.Time without
// pulling time.Time into every call site.
func metav1Zero() metav1.Time { return metav1.Time{} }
