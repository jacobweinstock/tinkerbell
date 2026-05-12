# v1alpha1 → v1alpha2 Migration Plan

> Living document. The **Status** table below is updated and committed
> at every phase start and completion so this plan doubles as a
> resumable checkpoint trail. If you are picking this up from a fresh
> context, read the table, then jump to the first non-`done` step.

## Status

Legend: `not-started`, `in-progress`, `done`, `blocked`.

| Step | Description                                                                          | Status      | Notes / Commit |
|------|--------------------------------------------------------------------------------------|-------------|----------------|
| 0    | Save plan + checkpoint baseline                                                      | done        | b39047af       |
| 1    | `pkg/migrate/transform/` — pure v1alpha1↔v1alpha2 transform functions                | in-progress |                |
| 2    | `pkg/migrate/runner/` — streaming runner with resumable `state.json`                 | not-started |                |
| 3    | `pkg/migrate/report/` — `--report json` and `--report tui` renderers                 | not-started |                |
| 4    | `crd` package: additive vs final modes; `DeleteCRDs`                                 | not-started |                |
| 5    | `cmd/tinkerbell`: `tinkerbell migrate` subcommand (argv[1] dispatch)                 | not-started |                |
| 6    | Default `crd.NewTinkerbell` to v1alpha2; refuse v1alpha1 data at normal startup      | not-started |                |
| 7    | Per-component v1alpha2 import migration (smee, tootles, tink-server, ui, backend...) | not-started |                |
| 8    | CI static-import-guard for `api/v1alpha1`                                            | not-started |                |
| 9    | Kind e2e verification                                                                | not-started |                |

---

## Background & key decisions

- The Tinkerbell binary installs its own CRDs via `crd.MigrateAndReady`
  (gated by `--enable-crd-migrations`). Helm does not install CRDs.
- v1alpha2 is the long-lived target. v1alpha1 will be **removed in the
  same release** that introduces v1alpha2 (hard upgrade).
- Migration is a one-shot operation per cluster. The binary's
  `migrate` subcommand performs it.
- **No conversion webhook.** Considered and rejected — only covers 3
  of 7 kinds and costs permanent infrastructure for partial UX benefit.
- v1alpha1 Go structs **remain in the repo** for the migration's
  lifetime — `pkg/migrate/transform/` uses typed structs in/out, not
  unstructured.
- The migrate command is fully **idempotent and resumable** via a
  fixed on-disk workdir + state file. No timestamped dirs. No
  `--skip-*` flags. No `--proceed`.

## Kind inventory

Each kind has a **handling** that determines what the migration does
with it:

- **apply** — declarative spec. Export, transform spec to v1alpha2,
  apply to the cluster.
- **archive** — execution record. Export and write to disk for audit;
  **never apply**. Re-applying would re-trigger execution
  (Workflow) or restart in-flight BMC operations (BMC Job). Per-kind
  detail on what is written to the archive:
  - `workflows`: **spec transformed** to v1alpha2; status field is
    dropped entirely.
  - `bmc.jobs`: **verbatim v1alpha1 YAML** — no transform.
- **drop** — no v1alpha2 successor. Log count and discard.

| v1alpha1 CRD                       | v1alpha2 CRD                  | Mechanism              | Handling |
|------------------------------------|-------------------------------|------------------------|----------|
| `tinkerbell.org/hardware`          | `tinkerbell.org/hardware`     | 1:1, same CRD          | apply    |
| `tinkerbell.org/templates`         | `tinkerbell.org/tasks`        | 1:N split, renamed CRD | apply    |
| `tinkerbell.org/workflowrulesets`  | `tinkerbell.org/policies`     | 1:1, renamed CRD       | apply    |
| `bmc.tinkerbell.org/machines`      | `tinkerbell.org/bmcs`         | 1:1, renamed CRD       | apply    |
| `tinkerbell.org/workflows`         | `tinkerbell.org/workflows`    | spec-only transform    | archive  |
| `bmc.tinkerbell.org/jobs`          | `bmc.tinkerbell.org/jobs`     | verbatim copy          | archive  |
| `bmc.tinkerbell.org/tasks`         | —                             | no successor           | drop     |

Archive output is **reference material only**. Operators wanting
historical visibility can inspect `target-v1alpha2/archive/`. They
may choose to manually apply selected files post-migration if they
understand the consequences (Workflow re-execution; BMC Job replay).

### Workflow spec transform notes

The Workflow.Spec rewrite (`TemplateRef` → `Tasks: []WorkflowTask`,
BootOptions field renames) depends on knowing how each v1alpha1
Template decomposed into v1alpha2 Tasks during the Template→Tasks
fan-out. The transform package exposes the Workflow function with an
injected lookup so it remains pure:

```
Workflow(src *v1alpha1.Workflow, refs TemplateRefs) (*v1alpha2.Workflow, error)
```

The runner builds `TemplateRefs` (a `map[templateName][]SimpleReference`)
during the Template transform phase and passes it to the Workflow
transform phase.

Workflow.Status is **not** transformed. The output object has an
empty status; this is acceptable because the archive copy is never
applied.

## Subcommand model

- `tinkerbell <flags>` — existing behavior, unchanged. No `serve`
  subcommand.
- `tinkerbell migrate <flags>` — new one-shot migration. argv[1]
  dispatch.
- Startup-time safety preflight in the normal `tinkerbell` mode
  refuses to start if v1alpha1 data is detected, pointing the user
  at `tinkerbell migrate`.

## `tinkerbell migrate` CLI

Flags:

- `--workdir <dir>` (required) — fixed directory for the migration.
  Resume reuses the same value.
- `--kubeconfig`, namespace, QPS/Burst — same shape as the existing
  kube backend flags.
- `--force` — wipe an existing non-empty workdir and start over.
  Refuses without this flag.
- `--dry-run` — run only the safe phases (export + transform).
  Refuses to advance past phase 2.
- `--report <format>` — `tui` (default, interactive visual report)
  or `json` (machine-readable to stdout).

## Workdir layout

```
<workdir>/
  state.json                       # phase tracker; atomic-rename updates
  source-v1alpha1/
    hardware/<ns>__<name>.yaml
    template/<ns>__<name>.yaml
    workflowruleset/<ns>__<name>.yaml
    bmcmachine/<ns>__<name>.yaml
    workflow/<ns>__<name>.yaml          # archive only
    bmcjob/<ns>__<name>.yaml            # archive only
    bmctask/<ns>__<name>.yaml           # dropped; kept for audit
  target-v1alpha2/
    hardware/<ns>__<name>.yaml
    task/<ns>__<tmplname>__<idx>.yaml
    policy/<ns>__<name>.yaml
    bmc/<ns>__<name>.yaml
    archive/
      workflow/<ns>__<name>.yaml        # transformed, never applied
      bmcjob/<ns>__<name>.yaml          # transformed, never applied
  logs/
    export.log
    transform.log
    apply.log
    crd.log
```

The `target-v1alpha2/archive/` subtree exists so a glob-walk in the
apply phase (`target-v1alpha2/*/*.yaml`) cannot accidentally pick up
archive-only files.

## `state.json`

```jsonc
{
  "version": 1,
  "phases": {
    "export":              {"hardware": "done", "workflow": "in_progress" },
    "transform":           {"hardware": "done" },
    "apply_crds_additive": "done",
    "apply_objects":       {"hardware": "done" },
    "delete_old_crds":     "pending",
    "apply_crds_final":    "pending"
  },
  "counts": {
    "hardware": {"exported": 1247, "transformed": 1247, "applied": 0}
  }
}
```

Single source of truth for resume. Updated atomically (write-temp +
rename).

## Execution phases

| # | Phase                  | Reversible?  | Description |
|---|------------------------|--------------|-------------|
| 1 | `export`               | yes          | Paged LIST every v1alpha1 kind; write each object to `source-v1alpha1/<kind>/<ns>__<name>.yaml`. Memory ceiling = one page (500). Per-file write = `O_EXCL` + rename-from-tmp. Skip if file exists and uid matches. |
| 2 | `transform`            | yes          | Walk `source-v1alpha1/<kind>/`, decode → typed transform → encode. Apply-handling kinds go to `target-v1alpha2/<kind>/`. Workflow goes to `target-v1alpha2/archive/workflow/` with spec transformed and status omitted. BMC Job is copied verbatim to `target-v1alpha2/archive/bmcjob/`. `bmc.Task`: log and discard. One file in memory at a time. Template→Tasks fan-out. |
| 3 | `apply_crds_additive`  | yes          | Apply v1alpha2 CRDs in additive mode: shared-name CRDs gain v1alpha2 (storage=true) while keeping v1alpha1 served; renamed CRDs created fresh. |
| 4 | `apply_objects`        | partially    | Server-side apply files under `target-v1alpha2/<kind>/` (hardware, task, policy, bmc) with field manager `tinkerbell-migrate`. **Files under `target-v1alpha2/archive/` are never applied.** Per-file completion recorded in `state.json`. |
| 5 | `delete_old_crds`      | **no**       | Delete `templates.tinkerbell.org`, `workflowrulesets.tinkerbell.org`, `machines.bmc.tinkerbell.org`, `tasks.bmc.tinkerbell.org`. Apiserver GCs CRs; workdir is the only recovery copy. |
| 6 | `apply_crds_final`     | **no**       | Re-apply v1alpha2 CRDs in final mode: shared-name CRDs lose v1alpha1 from `spec.versions`. |
| 7 | `report`               | —            | Print final report (TUI or JSON). |

Resume from any failed phase. Within a phase, per-object idempotency:

- **Export**: existing file + matching uid = skip; mismatched uid =
  fail with operator guidance.
- **Transform**: deterministic encode; rewrite is safe.
- **Apply**: server-side apply is idempotent; per-file done marker
  prevents re-work on resume.
- **CRD ops**: apply is idempotent by definition; delete is
  idempotent (404 = ok).

## Final report

Two output modes selected via `--report`.

### `--report json`

Single JSON document to stdout. Schema documented in
`pkg/migrate/report/`. Shape:

```jsonc
{
  "workdir": "/path/to/workdir",
  "started_at": "...",
  "completed_at": "...",
  "outcome": "success" | "partial" | "failed",
  "phases": { /* same shape as state.json */ },
  "kinds": [
    {
      "source": "hardware.tinkerbell.org/v1alpha1",
      "target": "hardware.tinkerbell.org/v1alpha2",
      "handling": "apply",
      "exported": 1247,
      "transformed": 1247,
      "applied": 1247,
      "skipped_resume": 0,
      "failed": 0,
      "errors": []
    },
    {
      "source": "workflows.tinkerbell.org/v1alpha1",
      "target": "workflows.tinkerbell.org/v1alpha2",
      "handling": "archive",
      "exported": 312,
      "transformed": 312,
      "applied": 0,
      "skipped_resume": 0,
      "failed": 0,
      "errors": []
    }
  ],
  "discarded": [
    {"source": "tasks.bmc.tinkerbell.org/v1alpha1", "reason": "no v1alpha2 successor", "count": 240}
  ]
}
```

### `--report tui` (default)

Rendered with `github.com/charmbracelet/lipgloss` plus `bubbletea`
for live progress during the run. Final report is a static,
terminal-respecting layout:

- Header panel: workdir, wall time, overall outcome.
- One-row-per-kind table with columns
  `source kind | target kind | handling | exported | transformed | applied | skipped | failed`.
  Right-aligned numeric columns. `applied` column shows `—` for
  archive- and drop-handling rows. Color cues when `failed > 0`.
  **No `->` arrows** — the source/target relationship is encoded as
  two adjacent columns.
- An "Archived" panel listing the kinds whose transformed files were
  written but not applied, with their counts and on-disk paths.
- A "Discarded" panel for `bmc.Task` showing count + reason.
- A "Next steps" panel with a single concrete instruction (restart
  command, or specific resume guidance on partial completion).

During the run, TUI mode shows a live bubbletea view with a progress
bar per kind for the active phase. JSON mode emits structured log
lines (one per significant event) to stderr during the run.

## Implementation steps

Each step is a self-contained commit. Pause between steps for user
review. No squashing.

| # | Scope | Commit message |
|---|-------|----------------|
| 1 | `pkg/migrate/transform/` — pure functions; table-driven tests with concrete YAML fixtures.                       | `migrate: add v1alpha1→v1alpha2 transform functions` |
| 2 | `pkg/migrate/runner/` — workdir layout, `state.json`, export/transform/apply phases with per-phase resume.       | `migrate: add streaming runner with resumable state` |
| 3 | `pkg/migrate/report/` — `json` and `tui` renderers over a common `Report` model.                                 | `migrate: add json and tui reports`                  |
| 4 | `crd` — additive vs final modes; `DeleteCRDs(names []string)`. Existing `MigrateAndReady` keeps working.         | `crd: support additive/final modes and CRD deletion` |
| 5 | `cmd/tinkerbell` — `tinkerbell migrate` subcommand via argv[1] dispatch.                                         | `cmd/tinkerbell: add migrate subcommand`             |
| 6 | Default `crd.NewTinkerbell` to v1alpha2; startup safety preflight refuses to start if v1alpha1 data is detected. | `crd: default to v1alpha2; refuse v1alpha1 data at startup` |
| 7 | Per-component v1alpha2 import migration (multiple sub-commits): smee, tootles, tink-server, ui, pkg/backend, controllers. | `<component>: migrate to v1alpha2 API`        |
| 8 | CI static-import-guard: no file outside `pkg/migrate/` and `pkg/api/` may import `api/v1alpha1`.                 | `ci: forbid v1alpha1 imports outside migration`      |
| 9 | Kind e2e verification: seed v1alpha1 release, run `tinkerbell migrate`, assert v1alpha2 state, restart normally. | (CI changes only)                                    |

## Checkpoint protocol

For every step:

1. Update the **Status** row to `in-progress`, commit:
   `docs: checkpoint step N in-progress`.
2. Do the work.
3. Update the row to `done` with the implementation commit's short
   SHA in the Notes column, commit:
   `docs: checkpoint step N done`.

A fresh context can resume by reading the Status table and starting
at the first non-`done` step.

## Open / deferred

- A separate `tinkerbell migrate status --workdir=…` subcommand that
  prints state without acting. Likely yes; cheap. Deferred to after
  step 9.
- Eventual removal of the `api/v1alpha1` Go package. Deferred — kept
  while the migrate command exists.
