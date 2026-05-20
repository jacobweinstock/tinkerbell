# Distributed tracing

Tinkerbell emits OpenTelemetry traces and logs over OTLP/gRPC. When an
OTLP collector endpoint is configured, every `Workflow` reconciliation
produces a single distributed trace that follows the workflow from the
first controller reconcile through BMC actions, network boot, in-OS
agent actions, metadata-service requests, and post-provision BMC
cleanup -- all sharing one `trace_id`.

## Configuration

The unified `tinkerbell` binary owns OTel initialization for every
in-cluster service (smee, tootles, tink-server, tink-controller, rufio,
secondstar). Agents running on booted hardware also export, but via
tink-server (see [Agent export path](#agent-export-path) below).

| Flag                  | Env var                          | Default | Effect |
| --------------------- | -------------------------------- | ------- | ------ |
| `--otel-endpoint`     | `TINKERBELL_OTEL_ENDPOINT`       | `""`    | OTLP/gRPC collector address, e.g. `otel-collector.observability:4317`. Empty disables OTel entirely (all instrumentation becomes no-op). |
| `--otel-insecure`     | `TINKERBELL_OTEL_INSECURE`       | `false` | Disable TLS to the collector. |
| `--otel-logs-enabled` | `TINKERBELL_OTEL_LOGS_ENABLED`   | `true`  | Also export application logs over OTLP. Logs continue to be written to stdout regardless. No-op when `--otel-endpoint` is empty. |

## Trace shape

For one `Workflow` lifecycle the trace contains two phase root spans
(`preBMC` and `postBMC`) linked under a single `workflow.lifecycle`
root, with per-component children:

```text
workflow.lifecycle                                     (tink-controller)
├── workflow.reconcile (xN)                            (tink-controller)
├── workflow.preBMC
│   └── rufio.job.reconcile                            (rufio)
│       └── rufio.task.reconcile
│           ├── bmc.set_boot_device
│           └── bmc.set_power_state
├── DHCP Packet Received: …                            (smee, via Hardware annotation)
├── smee.ipxe.serve_script                             (smee, via Hardware annotation)
├── action.execute (xN)                                (tink-agent, via kernel cmdline)
├── tootles.backend.GetEC2Instance                     (tootles, via Hardware annotation)
└── workflow.postBMC
    └── rufio.job.reconcile → bmc.set_power_state      (rufio)
```

`preBMC` and `postBMC` span ends are written back to
`Workflow.status.phaseTraceparents` and carry `trace.Link`s to each
other for readability in Jaeger / Tempo / Honeycomb UIs.

## How stitching works

Every component shares one `trace_id` via the same mechanism: the W3C
`traceparent` value of an upstream span is written to a well-known
location, and the downstream component reads it and uses it as the
parent for its own spans.

| Carrier                                                | Producer                           | Consumer                  |
| ------------------------------------------------------ | ---------------------------------- | ------------------------- |
| `Workflow.status.traceparent`                          | workflow controller (first reconcile) | workflow controller itself, every subsequent reconcile |
| `Workflow.status.phaseTraceparents[preBMC\|postBMC]`   | workflow controller                | workflow controller phase logic |
| `Job.metadata.annotations[tinkerbell.org/traceparent]` | workflow controller                | rufio                     |
| `Task.metadata.annotations[tinkerbell.org/traceparent]`| rufio                              | rufio                     |
| `Hardware.metadata.annotations[tinkerbell.org/traceparent]` | workflow controller (every reconcile) | smee (DHCP + iPXE HTTP), tootles |
| Kernel cmdline `tinkerbell_traceparent=…`              | smee iPXE script template          | tink-agent                |
| gRPC `traceparent` metadata header                     | otelgrpc (automatic)               | tink-server, tootles, etc. |

The Hardware annotation is the key indirection: smee and tootles look
up Hardware by source IP / MAC anyway, so they get traceparent stitching
"for free" without any new RBAC or extra k8s reads. The workflow
controller re-stamps it every reconcile so the annotation self-heals
and reflects the workflow's *root* traceparent (stable across the full
lifecycle).

## Agent export path

The tink-agent runs on a booted target machine, which typically has only
one outbound network route the operator can rely on: back to tink-server
over its gRPC port. Forcing every operator to also expose their OTel
collector to the provisioning network would be operationally painful.

Instead:

1. tink-agent's OTLP exporter is pointed at the same gRPC endpoint
   it already uses to call `WorkflowService`.
2. tink-server registers an OTLP `TraceService` + `LogsService` receiver
   on that gRPC server, which forwards every request **verbatim** to the
   operator-configured upstream collector (the same `--otel-endpoint`
   above).
3. Forwarding raw protobuf preserves span IDs, parent links, timestamps,
   and resource attributes exactly as the agent recorded them, so
   `action.execute` spans appear as proper children of the workflow
   trace rather than a disconnected island.

When `--otel-endpoint` is empty the relay is not registered and the
agent's OTLP client simply gets `Unimplemented` for those services; the
agent treats this as a soft failure and keeps running.

## Finding a workflow's trace

```bash
kubectl get workflow -n tink my-workflow -o jsonpath='{.status.traceparent}'
```

The W3C traceparent has the form `00-<trace_id>-<span_id>-<flags>`.
Search your tracing backend for the `<trace_id>` portion.

## Legacy carriers

`smee/internal/dhcp/otel` retains a `TraceparentFromContext` helper that
encodes a traceparent into the binary form required by DHCP option 43
suboption 69. It is not on any current code path; the kernel-cmdline
carrier (`tinkerbell_traceparent=…`) is the only supported way to carry
a traceparent into the booted OS and works regardless of how the
machine boots (iPXE chain, HTTP boot, etc.).

## Disabling

Leave `--otel-endpoint` unset. Every component still starts and logs to
stdout as JSON; the new `Workflow.status` fields stay empty; no
annotations are written; no gRPC overhead is added.
