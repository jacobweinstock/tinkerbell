# Workflow Cancellation

This document 

## Context

Tinkerbell provisions machines by having an agent on each machine execute an ordered list of Actions that make up a Workflow.
The agent talks to tink-server, which is the authority for what runs next and the durable record of execution progress.

### Current contract
The tink-server ↔ agent contract is minimal and poll-based. It is a two-method gRPC service:

```proto
service WorkflowService {
  rpc GetAction(ActionRequest) returns (ActionResponse) {}          // unary poll
  rpc ReportActionStatus(ActionStatusRequest) returns (ActionStatusResponse) {}
}
```

The agent loop (tink/agent/agent.go, (*Config).Run) is strictly synchronous: GetAction (poll + backoff when empty) → run the action to completion → ReportActionStatus → repeat.
There is no server→agent push channel; the agent only talks to the server between actions, never while one is executing.

### Problem statement

In the current model there is no way to cancel a running Action, in-flight Actions always runs to completion.
There is no server→agent channel during execution, so cancelling a Workflow only takes effect at the next `GetAction`.

## Goals and non-goals

### Goals

- Agent backward compatibility.
- Provide in-Action Workflow cancellation.

### Non-goals

- Introducing a new message broker, pub/sub system, or inbound agent endpoint.

## Design principles

### Three distinct channels with different scaling profiles

| Channel | Nature | Scaling profile |
|-------- | ------ | --------------- |
| **Assignment** | "here is your next action" | bursty; must be cheap while idle |
| **Status** | progress / results | frequent; one report per action transition |
| **Control** | cancel, reconfigure, ping | rare; agent polls while executing |

### The agent is mostly-idle and intermittently connected

Generally, an agent matters intensely for a few minutes during a provision and is idle or absent otherwise.
At larger scales, the agent's dominant cost is **idle presence**, not active work.
True concurrent provisioning is assumed to be far smaller than the fleet size.
The ideal design makes **idle cheap** and only transitions an agent from a cheap dormant state into an active, resource-consuming one for active agents.

## Proposed solution

### Overview

The proposed solution keeps action-level assignment and adds a separate control-poll operation, `PollCommands`, that the agent calls while executing an action.

```proto
service WorkflowService {
  rpc GetAction(ActionRequest) returns (ActionResponse) {}           // action assignment
  rpc PollCommands(PollCommandsRequest) returns (PollCommandsResponse) {} // cancellation/control while executing
  rpc ReportActionStatus(ActionStatusRequest) returns (ActionStatusResponse) {}
}
```

- **Cancellation works during execution.** The agent runs `Execute` and `PollCommands` concurrently. The server returns cancellation when the agent asks, without requiring tink-server to watch Kubernetes or the controller to call a public cancellation endpoint.
- **Zero new operational dependency.** No broker to deploy or monitor, no long-lived stream infrastructure, no new client library, and no new auth/subject model.

### Cancellation field in the Workflow CRD

### Workflow execution and cancellation

1. Cancellation is configured in the Workflow CR in Kubernetes.
2. While executing, the agent independently calls `PollCommands`.
   tink-server reads the current Workflow state and returns a matching cancellation command when cancellation is requested.
3. The agent's command-polling loop cancels the action context and kills the container (`docker stop` / containerd kill / k8s Job delete).
4. The agent reports a `Cancelled` status via `ReportActionStatus`; tink-server records it and the controller reconciles the durable state.

This requires the new agent core loop to run action execution and `PollCommands` concurrently.
The cancellation path is independent of the next assignment request, so it works while an action is running.

### Action-level assignment

`GetAction` returns one action at a time.
The response is the assignment checkpoint: the server updates the existing Workflow status before returning the action, and the agent reports the action transition after execution.
The next assignment is not available until the current action's durable state permits it.

`PollCommands` includes the Agent ID, Workflow ID, and current Action ID.
The server validates those identifiers against the current Workflow status before returning a command. This rejects commands for unknown, unassigned, or stale work.

Each `ReportActionStatus` request includes the Workflow ID and Action ID; the server validates both against the current Workflow state before accepting the event.
This rejects stale status from an earlier Workflow or status for an action that is not part of the assigned Workflow.

The Workflow and Action IDs gate **which work** a message can affect; they do not authenticate which process sent it.
Duplicate Agent IDs remain an operational identity problem outside this protocol's guarantees.

### Agent execution model

The new agent runs action execution and command polling concurrently:

```text
GetAction → action
  ├─ Execute(action)
  └─ PollCommands(workflow_id, action_id)
       └─ cancel execution context when a command arrives
ReportActionStatus → result
```

The command poll is cancelled when the action finishes.
Commands are at-least-once and must be idempotent. A terminal `Cancelled` state and runtime kill semantics (`docker stop` / containerd kill / k8s Job delete) are required.

## Alternatives considered

### Within gRPC

- **Action-level poll plus concurrent command poll (chosen).**
  `GetAction` remains the assignment operation. While executing the
  action, the agent independently calls `PollCommands` to receive
  cancellation and future control messages. This keeps the server as
  the durable action-cursor authority without requiring a stream,
  broker, or Kubernetes watch in tink-server. See "Proposed solution"
  below.
- **Whole-workflow assignment + server-streaming control.**
  `GetWorkflow` returns the complete ordered Workflow and
  `WatchCommands` carries control. This reduces assignment calls but
  adds larger payloads, stream lifecycle, reconnect, and agent cursor
  recovery complexity; it is not chosen for the initial design.
- **Server-streaming "hanging GET".** `Subscribe(req) returns (stream
  Work)`: one long-lived downstream for assignment + control; status
  stays unary. Half the complexity of bidi, still push + cancel.
- **Bidirectional stream.** Full duplex: `Connect(stream AgentMessage)
  returns (stream ServerMessage)`. Cleanest cancellation and push, at
  the cost of a long-lived stream (goroutine + connection) per agent
  and reconnect/resume complexity.

### Within HTTP (non-gRPC)

- **Server-Sent Events (SSE).** One long-lived server→agent
  `text/event-stream` for assignment + control; plain `POST` for
  status. Unidirectional, but that is all control needs. Extremely
  proxy/LB-friendly; lighter than WebSockets.
- **WebSockets.** Full duplex over HTTP upgrade; broad proxy support;
  equivalent to a bidi gRPC stream without gRPC.
- **Webhooks (server calls agent): ruled out.** Agents sit behind
  NAT/firewalls (BMC/edge networks) with no inbound reachability.

### Other substrates

- **MQTT.** Purpose-built for 100k–1M intermittently-connected
  devices. Pub/sub, tiny overhead, QoS, **Last-Will-and-Testament gives
  presence/liveness for free**, retained messages; cancel = publish to
  a device topic. If the frame is "IoT fleet at scale," this is the
  most first-principles-correct substrate.
- **NATS / JetStream.** Considered and **not chosen**. The strongest
  broker-based fit (embeddable, request/reply, per-agent subjects,
  and the agent already has a NATS transport stub), but it's a new
  operational substrate, a new client library, and a new auth/subject
  model to run and explain — ruled out in favor of extending the gRPC
  the fleet already speaks. See "Proposed solution" below.
- **QUIC / HTTP/3.** Connection migration + 0-RTT reconnect for agents
  on flaky networks; relevant if reconnect storms are a concern.
