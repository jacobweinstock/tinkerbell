# Workflow Cancellation

Status: partially implemented. Cancellation between Actions works for every
Agent on v1alpha1 (see [Implementation status](#implementation-status)).
In-Action cancellation (`PollCommands` and the Agent/runtime changes) is not
implemented.

This document proposes cancellation of an executing Workflow without adding a
broker, an inbound Agent endpoint, or a long-lived server-to-Agent stream. It
retains Action-level assignment and adds a concurrent, unary `PollCommands` RPC.
Cancellation is irreversible and is not rollback.

## Context

Tinkerbell provisions machines by having Agents execute an ordered sequence of
Actions. Tink Server assigns Actions and persists execution progress in the
Workflow's Kubernetes status.

The current [WorkflowService](../../../pkg/proto/workflow_service.proto) has two
unary RPCs:

```proto
service WorkflowService {
  rpc GetAction(ActionRequest) returns (ActionResponse) {}
  rpc ReportActionStatus(ActionStatusRequest) returns (ActionStatusResponse) {}
}
```

The [Agent loop](../../../tink/agent/agent.go) is synchronous: get an Action,
report `RUNNING`, execute it, report the result, and repeat. There is no
server-to-Agent control channel while an Action is executing. Cancelling an RPC
context is also not, by itself, proof that a runtime process stopped.

Before this design there was **no Workflow cancellation field or cancelled
state**. Disabling, deleting, or timing out a Workflow does not provide the
cancellation protocol described here. `spec.disabled` must not be repurposed as
cancellation.

The active [server](../../../tink/server/internal/grpc/grpc.go) and
[controller](../../../tink/controller/internal/workflow/reconciler.go) use the
[v1alpha1 API](../../../api/v1alpha1/tinkerbell/workflow.go). The repository also
contains [v1alpha2 types](../../../api/v1alpha2/tinkerbell/workflow.go); those types
alone do not mean that the active execution path has migrated. This proposal
targets v1alpha1 first and specifies the corresponding v1alpha2 changes.

## Goals and non-goals

### Goals

- Stop an Action while it is executing on a cancellation-capable Agent.
- Prevent subsequent Actions and Tasks from starting after cancellation is
  accepted, including Tasks assigned to another Agent.
- Preserve compatibility with older Agents and servers, with explicit limits.
- Distinguish a request to stop from confirmation that execution stopped.
- Survive duplicate commands, status-write conflicts, and process restarts.
- Keep idle polling cheap; add control traffic only for outstanding assignments.

### Non-goals

- Rollback, transactional provisioning, pause/resume, or undoing disk writes.
- Hard real-time interruption or stopping a disconnected Agent immediately.
- Cancelling already-issued BMC operations or reversing Hardware boot settings.
- Adding MQTT, NATS/JetStream, a public cancellation endpoint, or an inbound
  Agent listener.
- Exactly-once execution or resolving duplicate Agent identities.
- Adding cancellation to the file and NATS transports in the initial release.

## Contract and invariants

1. A cancellation request is sticky for the lifetime of a Workflow UID. A new
   Workflow is required to run the work again.
2. Once a request is committed, the server must not commit a new executable
   assignment for that Workflow. An assignment committed just before the request
   remains outstanding even if its RPC response has not reached the Agent.
3. An outstanding assignment is the persisted current Action in `PENDING` or
   `RUNNING`, not merely an Action that has reported `RUNNING`. Never infer
   inactivity from a missing start report.
4. `CANCELLING` means no further work may start, but assigned work may still be
   running. `CANCELLED` means there is no outstanding Agent execution. For a
   legacy Agent this is based on its existing terminal-report contract, not the
   stronger runtime confirmation available from a new Agent.
5. An offline Agent, ambiguous runtime state, or failed stop must not be turned
   into a successful cancellation by a timer.
6. Terminal results never regress. Cancellation does not rewrite a Workflow
   already in `SUCCESS`, `FAILED`, or `TIMEOUT`.
7. Workflow, Task, and Action identifiers select work; they do not authenticate
   the sender. This protocol does not strengthen the current Agent trust model.

## User-facing API

### Request

Add optional `spec.cancelled: boolean`, defaulting to false. Admission validation
must prohibit changing true back to false or removing it after it becomes true.
Repeated true requests are idempotent. Do not use an annotation as a second
cancellation API.

Use an optional boolean and a transition rule on `WorkflowSpec` that handles
omitted fields explicitly:

```cel
!(has(oldSelf.cancelled) && oldSelf.cancelled) ||
  (has(self.cancelled) && self.cancelled)
```

Omitted and false are equivalent before cancellation. Test this rule against
the generated CRD, including unrelated updates to existing objects that omit
the field.

For example:

```sh
kubectl patch workflow example -n tinkerbell --type=merge \
  -p '{"spec":{"cancelled":true}}'
```

This is a request, not an acknowledgement. Kubernetes accepting the patch does
not mean the Agent has stopped. Setting the field on an already-terminal
Workflow is harmless and leaves its result unchanged.

### Observed state

Add `CANCELLING` and terminal `CANCELLED` Workflow states. Add `CANCELLED` to
Action state and Task state where applicable. Preserve previously completed
Action results and leave never-assigned Actions pending; the overall Workflow
state explains why those Actions will never execute.

Add a `Cancellation` condition using the API's existing condition representation:

| Status | Reason | Meaning |
|---|---|---|
| `False` | `AwaitingAgent` | An assignment still needs a terminal acknowledgement. |
| `False` | `StopFailed` | The Agent could not confirm runtime termination. |
| `False` | `LegacyAgent` | Waiting for an old Agent to finish its current Action. |
| `True` | `NoOutstandingAction` | No assignment was outstanding when cancellation was reconciled. |
| `True` | `AgentStopped` | A capable Agent confirmed termination or confirmed it never started. |
| `True` | `LegacyActionFinished` | A legacy Agent reported its current Action terminal. |

Also add `status.cancellation.requestedAt` and
`status.cancellation.completedAt`. These are server/controller observation
timestamps, not the time the user submitted the patch. Set each once, preserve
it on retries, and set `completedAt` only when entering `CANCELLED`. Messages
identify the outstanding Agent, Task, and Action and any stop error.
If the server completes cancellation before the controller observes it, it sets
both timestamps in the same guarded status write.

A request observed after an existing terminal result does not create a
`Cancellation=True` condition: the result was completion, failure, or timeout,
not cancellation. Surface that distinction in the CLI and UI.

### Scope and operational limits

Cancellation covers Agent Actions throughout the Workflow, including a Task
transition between Agents. It takes precedence over disabled/preparing handling
for a nonterminal Workflow, so an unassigned, disabled Workflow can be cancelled
without rendering or booting it.

Do not enter success-only `POST` processing as a consequence of cancellation.
If cancellation is observed while already in `POST`, stop scheduling additional
post-operations and record `CANCELLED` once no Agent assignment is outstanding.
Already-issued BMC jobs may still run. The controller must check cancellation
before each external boot/post-operation; an operation already in flight can
race the request and cannot be recalled by this protocol.

No automatic reboot, power-off, ISO eject, or netboot restoration is implied.
Operators must inspect and recover partially provisioned Hardware before reuse.
In particular, `CANCELLED` is not proof that BMC activity or external side
effects have stopped.

## Protocol

### Additive changes

Keep the existing RPC signatures and field numbers. Add:

```proto
rpc PollCommands(PollCommandsRequest) returns (PollCommandsResponse) {}
```

The following fields are proposed additions, not existing generated APIs:

| Message | Additions |
|---|---|
| `ActionRequest` | `control_protocol_version`, set to `1` by a capable Agent. Absent/zero means legacy. |
| `ActionResponse` | Negotiated `control_protocol_version`, `workflow_uid`, and `recovery_only`. |
| `PollCommandsRequest` | `agent_id`, `workflow_id`, `workflow_uid`, `task_id`, `action_id`. |
| `PollCommandsResponse` | Optional `cancel_action` command; absence means no command. |
| `ActionStatusRequest` | `workflow_uid`, `control_protocol_version`, `stop_confirmed` on every version-1 terminal report, and optional `stop_error` for nonterminal stop-progress reports. |

`workflow_id` remains `namespace/name`, as in the existing protocol.
`workflow_uid` additionally fences deletion and recreation under the same name.
Task and Action IDs must match the persisted current assignment, not just an
entry somewhere in the rendered Workflow. A cancellation command carries the
same Workflow UID, Task ID, and Action ID, which the Agent checks before acting.

Append `CANCELLED = 6` to `ActionStatusRequest.StateType`; never renumber existing
values. Extend Agent event states, transport mappings, API state constants,
terminal predicates, and state display/serialization together.

The server records the negotiated control version with the assignment so it can
report legacy limitations and apply the correct acknowledgement rules. Re-serving
an assignment must not silently downgrade its recorded capability.
Keep the version originally negotiated for that assignment. If a new Agent
recovers a legacy assignment, it uses legacy reporting rules; for a cancelled
Workflow it receives recovery-only work, never a fresh executable assignment.
Its record remains legacy even if the new process obtains stronger stop evidence.
On a cancelled legacy recovery assignment, a new Agent that actively stops the
runtime reports existing `FAILED` with an explanatory message (or the actual
terminal outcome if already complete), not the new `CANCELLED` enum. The Workflow
still completes as `CANCELLED` under the legacy acknowledgement rule.
Only versions 0 and 1 are defined; reject unsupported requested versions rather
than guessing a compatible protocol.

### Negotiation and compatibility

| Agent | Server | Behavior |
|---|---|---|
| Old | Old | Unchanged; no cancellation. |
| Old | New | No new executable assignments after cancellation; the current Action runs to its ordinary terminal report. |
| New | Old | Missing negotiated version means legacy operation; no `PollCommands` or `CANCELLED` reports. Log that in-Action cancellation is unavailable. |
| New | New | Version 1 enables concurrent command polling and confirmed-stop reporting. |

An old Agent is not required to understand either new enum value. The new server
still accepts its existing terminal reports while cancellation is pending. Keep
the Action's reported result (`SUCCESS`, `FAILED`, or `TIMEOUT`) and complete the
Workflow as `CANCELLED` with reason `LegacyActionFinished`.

Legacy runtime cleanup is best-effort today. Its terminal report is the available
acknowledgement, not a new physical-stop guarantee. Operators needing confirmed
termination must upgrade Agents. If an old Agent disappears before reporting,
cancellation remains pending.

Every serving server and controller must understand the new contract before
accepting cancellation requests. A capable Agent must
not silently downgrade an assignment already negotiated as version 1 after
`Unimplemented` from another server; log a protocol/configuration error and retry
without starting more work. Downgrading servers with outstanding version-1 work
is unsupported. Old server status updates can drop fields unknown to their Go
types, so mixed old/new serving instances are unsafe even without a pending
cancellation. The maintenance-window rollout below avoids that interval.

### Server behavior and consistency

Follow [Agent and Tink Server Retry](AGENT_SERVER_RETRY.md): the server makes one
attempt per RPC; retry and jitter belong to the Agent.

- `GetAction` can retain its indexed cache lookup for candidate selection.
  Before committing an assignment, read the candidate from the API server,
  revalidate eligibility and cancellation, and write the assignment with a
  resource-version precondition. A concurrent spec patch invalidates that
  version. A conflict returns `Aborted`; it must not return the Action.
- Recovery candidate selection must include `CANCELLING` Workflows for version-1
  callers. Index outstanding assignments by their recorded Agent ID, retaining
  that identity until acknowledgement rather than moving the index to the next
  Task. Prioritize outstanding recovery over any new assignment to that Agent.
  Do not commit executable work for that Agent while cancellation recovery is
  outstanding, even if a different Workflow is eligible.
  Recovery-only responses must bypass normal executable-Action selection.
- Do not write assignment-related Hardware attributes for recovery-only
  responses or before the authoritative eligibility check. Defer those writes
  until assignment commitment. Discovery/enrollment remain distinct existing
  behavior, not a cancellation mechanism.
- `PollCommands` reads the Workflow directly from the API server, within the
  configured backend namespaces. This avoids unbounded informer staleness on
  the cancellation path. Reuse the existing
  [uncached Workflow reader](../../../pkg/backend/kube/workflows.go).
- A valid outstanding assignment with `spec.cancelled=true` receives
  `cancel_action`, even before the controller has set `CANCELLING`.
  A valid uncancelled assignment receives an empty response.
- `ReportActionStatus` reads authoritatively, validates the current assignment
  and negotiated version, and performs a resource-version-guarded status write.
  Do not retain the current permissive matching of any Action in the Task.
  The sole exception is an exact duplicate of an already-persisted terminal
  report: validate its stored identity and immutable result and return `OK`
  without changing the current assignment. This permits retries after the cursor
  has advanced without allowing old reports to change newer work.
- Both server and controller recognize the spec request directly; neither
  depends on the other having observed it first. Status writes use optimistic
  concurrency so neither can erase cancellation or advance a stale cursor.

The controller checks cancellation before its normal state switch for every
nonterminal state, including empty, `PREPARING`, `PENDING`, `RUNNING`,
`CANCELLING`, and `POST`. With no outstanding assignment it completes
cancellation directly; otherwise it waits without running preparation, Task
advancement, global-timeout transitions, or post-processing. Normal Workflow
spec/status watches trigger reconciliation; do not add predicates that suppress
cancellation patches or acknowledgement updates.

For an assignment already committed before cancellation, accept a matching
initial `RUNNING` report without reviving the Workflow. Legacy Agents may then
run that Action; capable Agents must keep polling and stop it. Rejecting a start
report is not sufficient proof of non-execution: an earlier `RUNNING` write may
have succeeded while its response was lost. Therefore a still-`PENDING`
assignment is not automatically settled on a rejected RPC or repeated poll.

The current assignment is the durable control target; do not add an in-memory
command queue. Return the same command on every valid poll until the assignment
is acknowledged. Cancellation requires no command IDs, broker acknowledgements,
or per-server ownership.

### Poll outcomes

| Result | Code | Agent behavior |
|---|---|---|
| No command or matching cancel command | `OK` | Continue or stop the named Action. |
| Malformed identifiers | `InvalidArgument` | Log a protocol error; stop locally and do not fetch more work until termination is confirmed. |
| Workflow missing or recreated with another UID | `NotFound` | Stop locally; never apply a report to the replacement Workflow. |
| Assignment no longer current, or unrelated terminal Workflow | `FailedPrecondition` | Stop the stale local execution; do not change the newer result. |
| Backend/transport failure | `Internal`, `Unavailable`, `DeadlineExceeded` | Retry with backoff; retain the assignment and continue execution unless independently cancelled/timed out. |
| Version-1 RPC unavailable | `Unimplemented` | Log version mismatch and retry; do not silently disable control. |

An authoritative missing/stale response means the Agent no longer has permission
to execute that assignment. A network failure is not such a response. This is an
intentional safety distinction: control-channel outages do not create a lease or
automatically kill otherwise valid work.
Stopping stale execution after an authoritative terminal response is an
intentional change for negotiated version-1 Agents: global `TIMEOUT` or `FAILED`
can now trigger local stop, although the existing terminal Workflow result is
preserved. Those states still do not mean that runtime termination was confirmed.
Legacy Agents retain their existing behavior.

## Agent execution and runtime contract

Use an optional command-reader transport interface so file and NATS execution
keep their existing behavior. Enable it only for negotiated version-1 gRPC
assignments. Do not make a no-op command reader appear cancellation-capable.

For a normal version-1 assignment:

```text
GetAction -> persist local assignment identity
  -> immediate PollCommands
  -> report RUNNING
  -> Execute(action) concurrently with PollCommands
  -> stop and join command polling
  -> report the final result until accepted or permanently rejected
```

The first poll closes the normal assignment-response delay before execution,
but is not an atomic start barrier. Cancellation can race container startup;
the polling loop must stop a container created during that race. If a request
is discovered before execution, do not start the runtime: report `CANCELLED`
with `stop_confirmed=true`.
For a new assignment, retry an unsuccessful initial poll before starting the
runtime; the outage rule below concerns execution that has already started.

Keep command polling active across image pulls, Action retries, and runtime
cleanup. A cancellation cause is distinct from an Action deadline or Agent
shutdown; use separate contexts/causes so `context.Canceled` alone cannot be
misclassified as user cancellation. Never retry an Action after cancellation.
If `RUNNING` reporting fails after a request races it, retain the assignment,
poll for cancellation, and settle it rather than discarding it.

When a command is accepted, cancel the execution context and request runtime
termination. A version-1 Agent sends **every** terminal report with
`stop_confirmed=true` only after the runtime confirms no execution remains,
including natural completion before any cancellation command is observed.
This prevents a cancellation patch racing an ordinary result from stranding
the assignment. Unconfirmed termination stays nonterminal at the protocol
level, even when the process appears to have exited.
Natural completion racing cancellation can retain its actual Action outcome;
the Workflow is still cancelled if its request won the persistence race.

Required runtime behavior:

| Runtime | Termination confirmation |
|---|---|
| Docker | Stop with a bounded grace period, force termination if needed, then inspect to confirm the container is neither running nor restarting. Removal failure alone is not execution failure if stopped state is confirmed. |
| containerd | Kill the task and wait for exit; do not treat sending a signal as confirmation. Preserve the runtime namespace for cleanup. |
| Kubernetes | Delete the owned Job and explicitly reconcile its owned Action Pods; do not depend solely on foreground garbage collection or Job absence. Confirm Pod termination with trustworthy runtime/node evidence. Force-deleted objects, a disconnected node, or ambiguous Pod state cannot establish physical termination. |

All three require bounded cleanup calls using a fresh context, not the cancelled
Action context. Use a 30-second stop-attempt budget for local runtimes; for
Kubernetes allow at least the configured Pod termination grace period plus
30 seconds for observation. Failure to confirm leaves
the Workflow `CANCELLING`, emits an explicit error, and prevents the Agent from
fetching another Action. Retry stop/reconciliation attempts with backoff.

Extend the runtime result contract to expose stop confirmation and stop errors;
the current error-only `Execute` interface and log-only deferred cleanup are not
sufficient to make these guarantees. Apply the same semantics during pull,
create, start, execution, and cleanup. A timed-out create/start call can have
taken effect remotely; reconcile by stable runtime identity rather than assuming
an RPC error means nothing was created.

An Agent can report `RUNNING` with `stop_confirmed=false` and explicit `stop_error`
while it retries cleanup. If cancellation is requested, the server keeps `CANCELLING` and sets
`Cancellation=False/StopFailed`; such a report must never advance the cursor.
It may mark the same assigned Action `RUNNING` if needed, but cannot select the
next Action, reset an existing execution-start timestamp, or change the
Workflow back to `RUNNING`.
The separate `stop_error` field distinguishes this from an initial start report;
diagnostic updates must preserve existing execution timestamps.
Without a cancellation request, retain the outstanding execution state and
surface the stop error in the Action message/logs; do not manufacture a user
cancellation condition or release the assignment.
The final report uses the Agent's parent context with a per-call deadline, not
the cancelled execution context. Join polling and cleanup goroutines before
releasing the assignment.

## State transitions and races

The first resource-version-guarded write decides an otherwise concurrent result:

| Situation when a cancellation request is observed | Outcome |
|---|---|
| No outstanding assignment, including between Tasks | Set `CANCELLED`; preserve completed Actions and do not assign the next one. |
| Assignment committed, including still `PENDING` | Set `CANCELLING`; wait for acknowledgement. |
| Capable Agent confirms stopped/not started | Record the Action result and set Workflow `CANCELLED`. |
| Capable Agent cannot confirm stopped | Remain `CANCELLING`; expose `StopFailed`. |
| Legacy Agent reports a terminal Action result | Preserve that Action result and set Workflow `CANCELLED` with the weaker legacy acknowledgement. |
| Workflow already `SUCCESS`, `FAILED`, or `TIMEOUT` | Preserve that existing terminal result. |
| Request committed before an Action result or global-timeout write | Cancellation wins for the Workflow; retain the Action's actual result if applicable. |
| Ordinary terminal result committed before the request | The terminal Workflow result wins. |

A successful intermediate Action report is not a terminal Workflow result.
Cancellation between that report and the next assignment prevents the next
Action, even if the previous Action succeeded.

Once `CANCELLING`, global timeout must not change the Workflow to `TIMEOUT` and
hide unconfirmed execution. An Action's own deadline may still terminate its
runtime; a confirmed terminal report can then settle the cancellation.

Terminal reports are idempotent. A lost response followed by an identical report
returns `OK` without changing timestamps or replaying post-operations. Reject
inconsistent results and stale Task/Action/UID combinations explicitly. A
duplicate `RUNNING` report after cancellation completion must not resurrect work.
Cancellation-aware reports with `stop_confirmed=false` cannot complete a pending
cancellation, even when their Action outcome is terminal.
Reject a version-1 terminal report without confirmation as `InvalidArgument`
without clearing the outstanding assignment. A capable Agent treats rejection
of its terminal report as a protocol fault: retain/quarantine the assignment,
log the fault, and do not fetch new work. Only nonterminal diagnostic reports
may carry an unconfirmed stop.

## Restarts, deletion, and recovery

- **Server/controller restart:** Recover from the Workflow spec, assignment,
  negotiated version, condition, and timestamps. No commands are lost because
  none exist only in memory.
- **Agent restart during cancellation:** A version-1 `GetAction` must return the
  already-outstanding assignment with `recovery_only=true`. This is not an
  executable assignment. The Agent must locate and stop its runtime resources,
  then acknowledge; it must never pull/start/retry the Action. A legacy caller
  receives no Action after cancellation.
- **Runtime recovery:** Persist assignment identity before starting resources
  and use stable, ownership-checked runtime identifiers/labels. Recovery must
  reconcile possible in-flight create operations. Label resources with Workflow
  UID, Task ID, Action ID, and Agent ID; containerd must use a deterministic
  resource ID or support lookup by those labels in the correct namespace.
  Persist the record in an Agent-owned state directory on durable storage when
  available, including the local host boot ID for Docker/containerd. A verified
  host reboot establishes that local processes from the earlier boot stopped;
  it says nothing about Kubernetes Jobs running elsewhere.
  Retain a durable stopped/not-started record until the final report is
  acknowledged, including when runtime resources have already been removed.
  Authoritative absence on a healthy local runtime can establish no remaining
  execution once any in-flight create is resolved. An absent local record or
  failed runtime lookup alone cannot. Kubernetes object absence alone is weaker
  than local-runtime absence: retain termination evidence rather than treating
  a force-deleted Pod on an unreachable node as stopped. Ambiguity leaves
  cancellation pending and requires operator inspection.
  On RAM-backed provisioning environments, local records may disappear across
  reboot. Recover identity from the server's outstanding assignment and reconcile
  the local runtime authoritatively; do not require a missing record to exist.
  Do not persist credentials or complete Action payloads in the recovery record.
- **Workflow deletion/recreation:** New Agents stop on authoritative `NotFound`
  or UID mismatch and log that reporting is no longer possible. Old Agents can
  continue until completion. Deletion is therefore not a cancellation API; ask
  for cancellation and wait before deleting. No cancellation finalizer is added
  in this proposal.
- **Offline Agent or irrecoverable runtime:** Keep `CANCELLING` indefinitely with
  the last useful diagnostic. Do not add an automatic force-complete timeout.
  Operators may stop/power off the machine out of band and recreate the
  Workflow only after verifying safety; deleting the old object loses the
  protocol's acknowledgement trail.

Neither stable Action IDs nor Workflow UIDs fence two live processes sharing an
Agent ID. This design assumes one active Agent process per identity; process
leases and execution-attempt fencing are separate work.

## Polling cost, latency, and observability

Use a proposed command-poll interval of 5 seconds, jittered uniformly in
`[2.5s, 7.5s)`, and a 5-second per-call deadline. Poll immediately when an
assignment is received. There is only one in-flight poll per assignment.
On transient errors, use the existing jittered exponential-backoff policy with
a default 30-second cap and a separate retry state from assignment/reporting.
Stop retries and timers when the assignment is settled.

For `A` actively assigned Agents and average interval `P`, steady healthy
control traffic is approximately `A / P` API-server reads per second. At 1,000
active Agents and a 5-second interval this is about 200 reads/second, in addition
to assignment and reporting. Idle Agents add no control polls. Benchmark the
API-server load before changing these defaults or targeting larger concurrency.

With healthy connectivity and a responsive API server, detection takes at most
one jittered poll delay plus one RPC's latency after a request commits. Total
completion also includes runtime termination, cleanup, and durable reporting.
There is no end-to-end bound during outages, runtime failures, or retries; the
5-second interval is not a five-second cancellation guarantee.

Log request observation, command delivery, local stop attempts, confirmation,
legacy limitations, and durable completion using Workflow UID/namespace/name,
Agent ID, Task ID, and Action ID. Record cancellation latency, pending age,
stop failures, legacy completions, and command-poll rates/errors with bounded
metric labels; do not use individual IDs as metric labels.

The UI and CLI must distinguish `CANCELLING` from `CANCELLED`, display the
condition reason, and warn about legacy acknowledgements and excluded BMC
operations. RBAC remains the existing permission to patch Workflow spec;
poll/report validation and configured namespace boundaries remain mandatory.

## Implementation and rollout

### Implementation status

The first increment is implemented for v1alpha1 and works with every existing
Agent. No Agent, protobuf, or v1alpha2 changes are part of it, so every
assignment is treated as a legacy (version 0) assignment:

- **API:** `spec.cancelled` with the sticky CEL transition rule, the
  `CANCELLING` and `CANCELLED` Workflow states, the `Cancellation` condition with
  the `LegacyAgent`, `NoOutstandingAction`, and `LegacyActionFinished` reasons,
  and `status.cancellation.requestedAt`/`completedAt`. A
  [test](../../../crd/workflow_cancel_test.go) evaluates the rule in the
  generated CRD.
- **Shared transitions:** the server and controller use
  [one package](../../../tink/internal/cancellation/cancellation.go) for the
  `CANCELLING`/`CANCELLED` transitions, so either can observe a request first.
- **Controller:** applies cancellation before its state switch. A cancelled
  Workflow is not rendered, prepared, moved to its next Task, timed out, or
  post-processed. It becomes `CANCELLED` immediately if no Action is outstanding,
  and `CANCELLING` otherwise.
- **`GetAction`:** never assigns an Action from a cancelled Workflow. While a
  cancelled, nonterminal Workflow still waits for this Agent's report, it also
  assigns no other Workflow's Action to the Agent, because nothing confirms
  the earlier Action stopped. The existing resourceVersion-conditional status
  write makes a cancellation that races an assignment win with `Aborted`.
- **`ReportActionStatus`:** for a cancelled Workflow, accepts reports only for
  the exact outstanding Agent, Task, and Action. `RUNNING` keeps the Workflow
  `CANCELLING`; `SUCCESS`, `FAILED`, or `TIMEOUT` records the Action's result
  and completes the Workflow as `CANCELLED` instead of advancing it or
  entering `POST`. `CANCELLED` is a terminal state for duplicate reports.
- **UI:** the status badge shows `CANCELLING` as in progress; `CANCELLED` uses
  the neutral style.

Known limits of this increment:

- An Action that is running continues until it finishes or reaches its own
  timeout.
- If the Agent loses an assignment before reporting it, for example because the
  Agent restarted or the `GetAction` response was lost, the Workflow stays
  `CANCELLING` and that Agent receives no new Actions. To recover, verify the
  machine is safe, then delete the Workflow.
- Strict current-assignment matching applies only to cancelled Workflows.
  Reports for Workflows that are not cancelled keep the existing matching.

The remaining work below must ship before advertising in-Action cancellation.
The rollout requirements in this section apply to the first increment too:
install the CRD and upgrade every server and controller before anyone sets
`spec.cancelled`.

### Remaining work

1. **API and schema:** Add assignment capability metadata to v1alpha1. Add
   equivalent fields and states to v1alpha2, appending integer state values
   rather than shifting existing ones. Cover conversion if/when that API
   version becomes served by the execution path.
2. **Protocol and server:** Regenerate protobufs, implement negotiation and
   `PollCommands`, strengthen assignment/report validation for all Workflows,
   and implement recovery-only responses and idempotent reports.
3. **Controller:** Handle the version-1 condition reasons (`AwaitingAgent`,
   `StopFailed`, `AgentStopped`).
4. **Agent and runtimes:** Add the optional command reader, concurrent polling,
   cancellation causes, durable recovery identity, confirmed-stop results, and
   cleanup retry/recovery for Docker, containerd, and Kubernetes. Preserve
   existing file/NATS behavior and ordinary non-cancelled execution.
5. **Consumers and operations:** Add a cancel action and condition reasons to
   the UI/CLI, examples, generated clients, and user-facing recovery
   instructions. Audit every terminal-state switch and enum mapping when new
   states are added.

### v1alpha2 mapping

The names above describe v1alpha1's string-valued Workflow status. In v1alpha2:

- Add `spec.cancelled` with the same irreversible semantics and validation.
- Append `StateCancelling` and `StateCancelled` after `StateTimeout`; add
  `WorkflowStateCancelling`, `WorkflowStateCancelled`, `TaskStateCancelling`,
  `TaskStateCancelled`, and `ActionStateCancelled` aliases and corresponding
  `"Cancelling"`/`"Cancelled"` string representations.
- Overall state lives in `status.metadata.workflow.state`; current assignment
  identity and state live in `status.metadata.task` and `status.metadata.action`,
  not v1alpha1's `status.currentState`. Preserve the rendered Task/Action metadata
  consistently when settling cancellation.
- Add `status.cancellation` timestamps and record negotiated capability alongside
  the current assignment metadata in each version. Capability belongs to an
  assignment, not to cancellation globally.
- Cancellation also gates per-Task preparing/post states and
  `spec.tasks[].hardware.bootOptions` operations. Do not run another Task's
  pre/post operations after observing the request.

Use a maintenance window for the initial rollout: pause Workflow spec writes and
drain/stop Agent-facing RPC ingress and old controllers, install the new CRDs,
replace every server/controller, and resume ingress and writes only after all
instances are upgraded and ready. Upgrade Agents afterward. Pausing must be
enforced operationally (for example, temporary API admission restrictions), not
just by hiding a UI button; users with direct Kubernetes access can otherwise
patch the field mid-upgrade. A zero-downtime mixed-version server/controller
rollout is not supported by this proposal.

Old Agents keep their existing execution behavior, with the documented
between-Action cancellation limit. Upgrade them before relying on in-Action
interruption or confirmed-stop reporting. The stronger terminal-confirmation
contract is enabled only by version-1 negotiation.

## Validation and acceptance criteria

The implementation must include:

- Schema tests for default false, repeated true, and rejection of true-to-false
  or field removal, in each served API version.
- Protocol tests for all four old/new combinations, additive enum compatibility,
  unsupported negotiated versions, and mixed-server rejection without downgrade.
- Server/controller race tests with injected barriers around assignment writes,
  cancellation patches, reports, and global timeouts. Assert that no executable
  assignment commits after the cancellation patch wins.
- Cases for unstarted, disabled, preparing, assigned-but-not-running, running,
  retrying, between-Task, `POST`, and already-terminal Workflows.
- Wrong Agent, Task, Action, Workflow UID, namespace, and stale-cursor tests;
  duplicate and conflicting final reports; lost RPC responses and write conflicts.
- Agent tests for an immediate command, command/start race, image pull, retries,
  natural completion, shutdown versus cancellation, hung polls, and report retry
  using an uncancelled context. Assert no goroutine leak or execution retry.
- Runtime integration tests proving termination and recovery for all three
  runtimes, including timed-out create/start, failed kill/delete, stuck Pod
  termination, and cleanup failure after the Action exits. An unconfirmed stop
  must leave `CANCELLING`, not `CANCELLED`.
- Restart tests at every assignment/execute/report boundary, including
  recovery-only handling that never starts work, and deletion/recreation tests
  proving old assignments cannot modify the replacement Workflow.
- Load tests measuring actual command read rates, API-server latency, detection
  latency, and cancellation completion latency. Check the stated poll-delay
  bound only under healthy conditions and separately verify outage behavior.
- Regression tests for uncancelled execution, existing retry policy, legacy
  Agents, success-only post-processing, and unchanged file/NATS transports.

## Alternatives considered

| Alternative | Trade-off and decision |
|---|---|
| Action-level assignment plus concurrent unary control poll | **Chosen.** Retains the durable server cursor and existing outbound gRPC connection, with bounded request lifetimes and load proportional to active assignments. |
| Whole-Workflow assignment plus server-streaming control | Fewer assignment calls, but adds Agent cursor persistence, larger payloads, and reconnect/resume complexity beyond cancellation. |
| Server-streaming or long-poll control | Lower detection latency or fewer reads while idle, but requires held-request lifecycle, wakeup/watch ownership, and reconnect handling. Revisit if measured unary polling costs justify it. |
| Bidirectional gRPC stream | Unified assignment/status/control, but substantially changes the current protocol and connection lifecycle. |
| SSE or WebSockets | Viable push channels, but introduce a second transport beside existing gRPC without resolving durable acknowledgement or runtime termination. |
| Webhooks/server calls Agent | Ruled out because provisioning Agents commonly sit behind NAT/firewalls and have no inbound reachability. |
| MQTT | Useful for intermittently connected fleets, but requires a broker, subject/identity model, and another operational dependency. |
| NATS/JetStream | Existing Agent transport code is not a deployed cancellation control plane. Adopting it for this feature still adds broker operations and delivery/authentication semantics. |
| QUIC/HTTP/3 | Connection migration may help unreliable networks, but changing transport alone does not supply cancellation, fencing, or durable acknowledgement. |

Revisit push control only with measured latency or API-server capacity evidence.
The Workflow remains the durable source of cancellation intent regardless of
the delivery mechanism.
