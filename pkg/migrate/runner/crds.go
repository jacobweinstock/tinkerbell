package runner

import "context"

// CRDInstaller is the runner's view of the CRD lifecycle. It is
// implemented by an adapter over crd.Tinkerbell in the migrate
// command; tests use a fake.
//
// Each method runs the corresponding migration phase end-to-end and
// returns nil on success. The runner records phase state around the
// call and treats a non-nil error as fatal for that phase.
type CRDInstaller interface {
	// ApplyAdditive applies the v1alpha2 CRD set with shared-name
	// CRDs serving both v1alpha1 and v1alpha2.
	ApplyAdditive(ctx context.Context) error

	// DeleteOld removes v1alpha1-only CRDs after object migration.
	// Implementations must be idempotent (404 on a missing CRD is
	// not an error).
	DeleteOld(ctx context.Context) error

	// ApplyFinal re-applies the v1alpha2 CRD set without v1alpha1 in
	// spec.versions on shared-name CRDs.
	ApplyFinal(ctx context.Context) error
}
