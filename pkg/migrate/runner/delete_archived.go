package runner

import (
	"context"
	"fmt"
	"path/filepath"

	"golang.org/x/sync/errgroup"
)

// runDeleteArchivedObjects deletes the v1alpha1 cluster CRs of every
// archive-handled source kind. This must run before
// apply_crds_final, which drops v1alpha1 from spec.versions on the
// shared-name CRDs (workflows, jobs.bmc); leaving v1alpha1-stored CRs
// behind would make `kubectl get` for those kinds fail with
// "request to convert CR from an invalid group/version".
//
// The list of objects to delete comes from the workdir
// (source-v1alpha1/<kind>/) so the phase is fully resumable and does
// not depend on the cluster still serving v1alpha1 at run time. The
// renamed CRDs (templates, workflowrulesets, machines.bmc, tasks.bmc)
// are GC'd by delete_old_crds and are not touched here.
func (r *Runner) runDeleteArchivedObjects(ctx context.Context, state *State) (rerr error) {
	r.progress.PhaseStart("delete_archived_objects")
	defer func() { r.progress.PhaseEnd("delete_archived_objects", rerr) }()
	for _, k := range SourceKinds {
		if k.Handling != HandlingArchive && k.Handling != HandlingArchiveVerbatim {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if state.Phases.DeleteArchivedObjects[k.Name] == PhaseDone {
			continue
		}
		state.SetDeleteArchivedObjects(k.Name, PhaseInProgress)
		if err := state.Save(r.layout); err != nil {
			return err
		}
		if err := r.deleteArchivedKind(ctx, k); err != nil {
			state.SetDeleteArchivedObjects(k.Name, PhaseFailed)
			_ = state.Save(r.layout)
			r.progress.KindEnd("delete_archived_objects", k.Name, err)
			return fmt.Errorf("delete archived %s: %w", k.Name, err)
		}
		state.SetDeleteArchivedObjects(k.Name, PhaseDone)
		if err := state.Save(r.layout); err != nil {
			return err
		}
		r.progress.KindEnd("delete_archived_objects", k.Name, nil)
	}
	return nil
}

func (r *Runner) deleteArchivedKind(ctx context.Context, k SourceKind) error {
	srcDir := r.layout.SourceKindDir(k.Name)
	files, err := listYAMLs(srcDir)
	if err != nil {
		return err
	}
	r.progress.KindStart("delete_archived_objects", k.Name, len(files))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(r.concurrency)
	for _, name := range files {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			ns, objName, ok := parseObjectFilename(name)
			if !ok {
				return fmt.Errorf("unexpected filename in %s: %s", srcDir, name)
			}
			namespaced := ns != ""
			if err := r.client.Delete(gctx, k.GVR, namespaced, ns, objName); err != nil {
				return fmt.Errorf("delete %s/%s: %w", filepath.Join(ns, objName), k.GVR.Resource, err)
			}
			r.progress.KindItem("delete_archived_objects", k.Name)
			return nil
		})
	}
	return g.Wait()
}
