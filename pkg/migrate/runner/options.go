package runner

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func listOpts(continueToken string) metav1.ListOptions {
	opts := metav1.ListOptions{Limit: listPageSize}
	if continueToken != "" {
		opts.Continue = continueToken
	}
	return opts
}

func applyOpts(fieldManager string) metav1.PatchOptions {
	force := true
	return metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        &force,
	}
}
