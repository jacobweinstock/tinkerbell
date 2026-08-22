# v1alpha2 Coexistence

**Status:** proposed
**Owners:** Tinkerbell maintainers
**Decision date:** TBD

## Problem

v1alpha2 reshapes the Tinkerbell API and replaces several CRDs. Operators need
to adopt and validate it without interrupting the v1alpha1 deployments they are
already running. A hard cutover, deleting v1alpha1 data, or refusing to start
when v1alpha1 objects exist does not meet that bar.

For the Kubernetes controllers this is well-understood: both CRD versions can be
served, users create either one, and a controller watches the version it
understands.

The unsolved part is everything that is **not** a Kubernetes client. Smee's
DHCP, TFTP and iPXE HTTP endpoints, Tootles' metadata HTTP endpoint, and tink
server's gRPC endpoint each bind a single address, and their clients identify a
machine with a MAC address, an IP address, an instance ID, or an agent ID. None
of those carry a Kubernetes API version. A DHCP DISCOVER cannot ask for
v1alpha2. So these services need a rule for deciding which API version to answer
from, and that rule is the subject of this document.

## Goals

- A `v1alpha1` deployment keeps working exactly as it does today, and requires
  no conversion webhook and no cert-manager.
- v1alpha1 and v1alpha2 resources can exist in one cluster and both be acted on.
- Every public listener binds once and answers each request deterministically.
- A Workflow or BMC Job is never progressed by two implementations.
- Nothing about v1alpha1 depends on migrating data to v1alpha2.
- v1alpha1 stays served, reconciled and documented for at least 12 months after
  v1alpha2 GA.

## Non-goals

- Executing one Workflow under both versions' semantics.
- Automatically deleting v1alpha1 CRDs or custom resources.
- Making the agent CRD-version-aware.
- Choosing the release that finally removes v1alpha1.

## Background: what an API version is and is not

Two facts drive the whole design, and both are easy to get wrong.

**A same-name CRD is one object with two representations.** `hardware`,
`workflows` and `jobs` keep their group and resource across versions, so
installing both versions means a single multi-version CRD. There is one stored
record per object. The API server converts it to whichever version the client
requested. The version an object was *created* with is not retained — if it is
not the storage version, it is converted on write and the original form is gone.

Consequently a client cannot ask "give me only the objects created as
v1alpha1"; no such attribute exists. Both versioned endpoints list the entire
collection.

**Conversion is done by the API server, not the client.** controller-runtime
resolves a Go type to a GVK and asks that version's endpoint
(`mapper.RESTMapping(gvk.GroupKind(), gvk.Version)`), then deserializes the
response. `For(&v1alpha1.Workflow{})` therefore guarantees the *shape* a
reconciler receives — never the wrong Go type — but it does not restrict *which*
objects arrive. Both informers see every object.

The hub/spoke split (`conversion.Hub`, `conversion.Convertible`) is a
controller-runtime concept used by its generic conversion webhook handler so
that N versions need 2(N-1) conversion functions instead of N². The API server
has no notion of a hub; it asks for a `desiredAPIVersion`. "Reconcile only the
hub" is sound advice for a single-version steady state, but it cannot express
coexistence, because it assumes the hub can represent everything — see
"Why `both` runs two engines".

## Decision

`--api-version` selects a deployment mode, and the mode determines which CRDs
are installed. This is what keeps the webhook out of single-version
deployments.

| Mode | CRDs installed | Conversion webhook | Reconcilers | Services read via |
| --- | --- | --- | --- | --- |
| `v1alpha1` (default) | v1alpha1 set, each single-version | **none** | v1alpha1 only | v1alpha1 |
| `v1alpha2` | v1alpha2 set, each single-version | **none** | v1alpha2 only | v1alpha2 |
| `both` | union of both sets | **required** | both, partitioned | v1alpha2 |

[crd/crd.go](crd/crd.go) already models this: `TinkerbellDefaults` and
`TinkerbellV1Alpha2` are separate single-version CRD sets keyed by
`CRDsByVersion`.

Only the three same-name CRDs become multi-version in `both`:

| CRD | v1alpha1 | v1alpha2 | In `both` |
| --- | --- | --- | --- |
| `hardware.tinkerbell.org` | yes | yes | multi-version, webhook conversion |
| `workflows.tinkerbell.org` | yes | yes | multi-version, webhook conversion |
| `jobs.bmc.tinkerbell.org` | yes | yes | multi-version, webhook conversion |
| `templates.tinkerbell.org` | yes | — | single-version, no conversion |
| `workflowrulesets.tinkerbell.org` | yes | — | single-version, no conversion |
| `machines.bmc.tinkerbell.org` | yes | — | single-version, no conversion |
| `tasks.bmc.tinkerbell.org` | yes | — | single-version, no conversion |
| `tasks.tinkerbell.org` | — | yes | single-version, no conversion |
| `policies.tinkerbell.org` | — | yes | single-version, no conversion |

Version-exclusive CRDs are distinct resources, so they never require conversion.

Conversion for the three shared CRDs must be pure and lossless, with fields that
have no counterpart preserved in conversion metadata. It must not dereference
`Hardware.spec.bmcRef`: a conversion function receives only the object and
cannot read the cluster. Folding a BMC Machine into `Hardware.spec.bmc` is the
migration command's job.

## Why `both` runs two engines

A single reconciler on one version cannot serve `both`, because conversion
between the Workflow versions is not semantically complete:

```go
// v1alpha1
type WorkflowSpec struct { TemplateRef string `json:"templateRef,omitempty"` ... }

// v1alpha2
type WorkflowSpec struct { Tasks []WorkflowTask `json:"tasks,omitempty"` ... }
type WorkflowTask struct { TaskRef SimpleReference `json:"taskRef,omitempty"` ... }
```

`Template` and `Task` are different CRDs, and the split is 1:N. A conversion
function cannot turn one into the other, so a v1alpha1 Workflow rendered as
v1alpha2 has no runnable tasks. If v1alpha2 were the only reconciler, such a
Workflow would stall until someone migrated it — which would make migration a
prerequisite for v1alpha1 to keep working. That is not acceptable, so `both`
runs both engines.

### How the two engines partition

No new field, label or annotation is introduced. The schemas already partition
the objects, structurally rather than by convention: v1alpha1 has no `tasks`
field and v1alpha2 has no `templateRef` field, so neither can be authored with
both, and conversion cannot fabricate the missing one.

```
v1alpha1 Workflow reconciler  →  acts iff spec.templateRef != ""
v1alpha2 Workflow reconciler  →  acts iff len(spec.tasks) > 0
```

Exactly one holds for any authored Workflow. BMC Job uses the equivalent test on
its own version-exclusive fields. This is applied with
`builder.WithPredicates(...)`; `cache.Options.ByObject` label or field selectors
are a later server-side optimisation, and `spec.versions[].selectableFields`
would be needed to select on a spec field.

A Workflow matching neither predicate is invalid, not unowned. It must surface a
status condition and an event rather than being silently skipped by both.

`both` therefore runs two informers and two reconcilers for the three shared
CRDs. That cost is confined to `both`; single-version modes run one of each.

## Non-CRD services

This is the part the mode table above answers with "services read via", and it
needs justification because these services have no way to be told a version.

### The rule

Each service reads through the **newest served version**, chosen once at
startup from the installed CRDs. With only v1alpha1 installed it reads
v1alpha1; with only v1alpha2, v1alpha2; in `both`, v1alpha2.

This works because of the first fact in the Background section: that version's
endpoint returns *every* object, including ones authored in the other version,
converted by the API server. A MAC, IP, instance ID or agent ID lookup therefore
cannot miss a machine and cannot see it twice. There is no per-request version
decision, no fan-out, and no ambiguity to arbitrate.

It depends on one property, which the lossless-conversion requirement already
implies: conversion must be complete for the fields these services consume —
DHCP and netboot configuration, iPXE data, and instance metadata. Unlike
`bmcRef`, these are plain field mappings with no cluster reads, so they can be
total. A gap here is a conversion bug and must be caught by round-trip tests,
because it would degrade service for whichever version is not being read.

### Consequences per service

Every service binds its listener once, in all three modes.

**Smee** resolves one Hardware per request: DHCP by client MAC; TFTP by MAC then
client IP; iPXE script and PXE-over-HTTP by MAC in the path then source IP. It
never consults two versioned backends for one packet — that risks two DHCP
offers, and there is no protocol field to disambiguate them.

**Tootles** resolves one instance per request, by source IP for the EC2
metadata routes and by instance ID for the optional instance endpoint. There is
no version-prefixed route, because a cloud-init or EC2 metadata client cannot
choose one.

**Tink server** registers `proto.WorkflowServiceServer` exactly once — two
implementations cannot both register `GetAction`. It reads Hardware through the
newest served version like the others. For Workflow it must respect the
invariant that **at most one Workflow per agent ID is active across both
versions**; more than one is an invalid cluster state, not a precedence
question. `GetAction` therefore:

1. lists active Workflows for `ActionRequest.agent_id`;
2. requires exactly one, returning `FailedPrecondition` and logging the agent ID
   and candidates if there are more;
3. delegates status reads and writes to the engine selected by that Workflow's
   spec shape, using the same predicate the reconcilers use;
4. falls back to auto-enrollment keyed on the Hardware when no Workflow exists;
5. returns `NotFound` when there is nothing to run.

For a duplicate identity — the same MAC, IP, instance ID or agent ID on more
than one object — every service fails closed. It logs the identity and the
candidates and returns no provisioning data, rather than picking by list order.

**The agent is unchanged.** It has no Kubernetes API imports and speaks only
gRPC, so it is version-agnostic. Proto additions for retries and background
execution are additive and wire-compatible, but background execution is not
behaviour-compatible with an agent that ignores the field: such an agent would
run a detaching action in the foreground and block the Workflow. The server must
require an advertised capability before emitting a background action.

## Rollout

1. Upgrade with `--api-version=v1alpha1`. Single-version CRDs, no webhook, no
   behaviour change.
2. To evaluate v1alpha2, switch to `both`. This installs the multi-version CRDs
   and requires the conversion webhook and its TLS material, which must be
   serving **before** the CRDs are converted, since every read and write of a
   non-storage version depends on it.
3. Author v1alpha2 resources, or run the migration command to create v1alpha2
   successors — Tasks from Templates, Policies from WorkflowRuleSets, embedded
   BMC data from BMC Machines — reporting BMC Tasks as unsupported. It is
   additive and idempotent, never deletes source objects, and is never required
   for v1alpha1 to function.
4. When nothing depends on v1alpha1 any more, move to `--api-version=v1alpha2`.
   Before v1alpha1 can be dropped from a shared CRD, existing objects must be
   rewritten at the v1alpha2 storage version and `status.storedVersions` must no
   longer list v1alpha1. Changing the CRD manifest does not rewrite stored
   records.
5. Removing v1alpha1 entirely is a separate release, after the support window.

Helm exposes the mode and the webhook certificate mode. Ordinary clusters use an
existing or chart-managed cert-manager; the embedded-apiserver deployment cannot
schedule cert-manager, so it uses an in-process rotator that keeps the CRD
`caBundle` current.

## Implementation plan

1. Wire `--api-version` to CRD installation so each mode installs its own set,
   and only `both` produces multi-version CRDs. Confirm `v1alpha1` installs
   nothing that references a webhook.
2. Implement the conversion spokes for `hardware`, `workflows` and `bmc/jobs`,
   with round-trip tests asserting completeness for every field the non-CRD
   services consume. Add the webhook wiring and the three certificate modes.
3. Give the services a reader that is selected once at startup from the newest
   served version, mapping the typed object into the existing DHCP/netboot,
   iPXE and metadata outputs. Do not introduce a version-neutral mirror of the
   Hardware or Workflow schema.
4. Add the duplicate-identity check and fail-closed behaviour for MAC, DHCP IP,
   instance ID and agent ID, with tests per lookup form.
5. Add the spec-shape predicates to the Workflow and BMC Job reconcilers, and
   the invalid-object condition for anything matching neither. Keep the
   v1alpha1 reconcilers behaviour-identical.
6. Make `GetAction` enforce the one-active-Workflow-per-agent invariant and
   delegate on spec shape.
7. Make the migration command additive-only, and add the verified storage
   rewrite as an explicit phase rather than a manifest edit.
8. Add proto retries and background fields plus agent capability negotiation,
   gated by `buf breaking`.
9. End-to-end coverage for all three modes: a v1alpha1 deployment that never
   contacts a webhook; one DHCP offer per request; ambiguous identity failing
   closed; `both` running a Template-based and a Task-based Workflow
   simultaneously; conversion round trips; migration idempotency; and an old
   agent never receiving a background action.
10. Publish the operator migration guide and the reference material for modes,
    certificate options, errors and the support timeline.

## Alternatives considered

**Reconcile only the hub version.** The standard single-version recommendation,
and correct for `v1alpha1`-only and `v1alpha2`-only modes. It cannot serve
`both`: the hub cannot represent a Template-based Workflow, so adopting it would
make migration a precondition for v1alpha1 to keep running.

**Route services by API version.** DHCP, TFTP, iPXE, EC2 metadata and the agent
gRPC contract have no field for it. Splitting by port, address or URL prefix
would require reconfiguring every client and partitioning the provisioning
network.

**Fan out each request to both versions.** Duplicate DHCP offers, conflicting
metadata, and two engines advancing one Workflow. Rejected.

**Distinguish objects by the version they were created with.** Not possible: the
creation version is discarded on write, and no selector can match it.

**A per-object ownership label.** Redundant. The schemas already partition
Workflows and BMC Jobs structurally, and a label would add a lifecycle,
defaulting rules for existing objects, and a new way to be wrong.

## Acceptance criteria

- A `v1alpha1` deployment upgrades, provisions normally, and never calls a
  conversion webhook.
- Each mode binds every public listener exactly once.
- In `both`, a Template-based Workflow and a Task-based Workflow both run, with
  no migration performed.
- Exactly one reconciler acts on any given Workflow or BMC Job; one matching
  neither predicate is reported, not ignored.
- A DHCP, TFTP, iPXE or metadata request returns one answer for one identity,
  and fails closed with a logged reason for a duplicate identity.
- An agent request advances at most one Workflow.
- Conversion round-trips preserve every field the services consume, and the
  migration command never deletes v1alpha1 data.
- No agent receives a background action without advertising support for it.
