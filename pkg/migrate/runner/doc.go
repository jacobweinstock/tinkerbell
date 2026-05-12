// Package runner drives a v1alpha1 → v1alpha2 Tinkerbell migration
// against a Kubernetes cluster and a local workdir. The runner is
// idempotent and resumable: it tracks per-phase, per-kind progress in
// workdir/state.json and re-running it after a partial run resumes
// where it left off.
//
// Phases are executed in order:
//
//  1. export — list v1alpha1 objects, write each as YAML to
//     workdir/source-v1alpha1/<kind>/<ns>__<name>.yaml.
//  2. transform — decode each source file, call the pure transform
//     functions in pkg/migrate/transform, write the result(s) to
//     workdir/target-v1alpha2/. Workflow goes to
//     target-v1alpha2/archive/workflow/; bmc.Job is copied verbatim
//     to target-v1alpha2/archive/bmcjob/; bmc.Task is dropped with a
//     log entry.
//  3. apply_crds_additive — add v1alpha2 to shared-name CRDs while
//     keeping v1alpha1 served. (Implemented in step 4 of the plan.)
//  4. apply_objects — server-side apply every file under
//     workdir/target-v1alpha2/<kind>/. Files under
//     target-v1alpha2/archive/ are intentionally skipped.
//  5. delete_old_crds — remove v1alpha1-only CRDs. (Step 4.)
//  6. apply_crds_final — drop v1alpha1 from shared-name CRDs. (Step 4.)
//
// The package operates on typed structs from the transform package
// for the transform phase and unstructured.Unstructured for export
// and apply, so it does not need a runtime.Scheme.
package runner
