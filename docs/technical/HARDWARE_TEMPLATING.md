# Configure Hardware Templating

Use this guide to enable v1alpha1 Hardware templating, configure which
referenced objects a Hardware may access, and check or refresh rendered values.
Reference policy is separate from Kubernetes RBAC: a policy rule does not grant
the Tinkerbell Service Account permission to read an object, and Service Account
permissions do not automatically authorize a Hardware reference. For complete
Quamina rule syntax and examples, see [Hardware References](REFERENCES.md). For
implementation details, see the [developer explanation](HARDWARE_TEMPLATING_DEVELOPMENT.md).

## Enable Hardware templating

Templating is disabled by default to preserve existing Hardware that contains
literal `{{` text, including cloud-init Jinja templates. Enable it for the
Tinkerbell process with:

```text
--backend-kube-hardware-templating-enabled
```

The corresponding Helm value is
`deployment.envs.globals.backendKubeHardwareTemplatingEnabled`, which defaults
to `false`. With templating disabled, the stored Hardware is served unchanged.

Once enabled, string values in Hardware `spec` fields are rendered as Go
templates, except for the lookup fields described below. A literal `{{` in a
rendered field must be written as `{{ "{{" }}`.

## Define references and templates

Declare each referenced Kubernetes object under `spec.references`. The key
(`cluster` below) is the name used to access that object in a template.

```yaml
spec:
  references:
    cluster:
      version: v1
      resource: configmaps
      namespace: tinkerbell
      name: cluster-network
  metadata:
    instance:
      hostname: '{{ .hardware.metadata.name }}.{{ .references.cluster.data.domain }}'
```

Templates can read:

- `.hardware` for the Hardware itself, including other fields in its `spec`.
- `.references.<name>` for each declared reference that passes policy and can
  be read by Kubernetes.
- The hermetic Sprig functions and Tinkerbell's template helper functions.

Hardware `.references` are local to Hardware rendering. Workflow templates read
the shared rendered Hardware snapshot; they do not receive `.references` or
`.hardware.spec.references`. A Workflow can use rendered Hardware fields, but
cannot inspect or resolve the Hardware's reference declarations or fetched
objects.

Paths use JSON field names and are case-sensitive. Use `index` for keys that
contain punctuation, such as Secret data keys:

```gotemplate
{{ index .references.credentials.data "client.der" | b64dec }}
```

Hardware identity and lookup fields are not rendered: `spec.references`,
`spec.agentID`, `spec.metadata.instance.id`,
`spec.interfaces[].dhcp.mac`, and `spec.interfaces[].dhcp.ip.address`. These
fields are used to find Hardware before rendering. `metadata` and `status` are
also not rendered. The implementation details and v1alpha2 rendering model are
described in the [developer explanation](HARDWARE_TEMPLATING_DEVELOPMENT.md) and
[v1alpha2 templating](v1alpha2/templating.md).

## Allow reference access

A Hardware reference does not grant access by itself. Add a rule that matches
the reference and the Hardware that declares it. Authorization is Hardware-wide:
if a reference is allowed, its resolved data is available to Hardware
templating wherever that Hardware is used. For the policy event fields and full
rule syntax, see [Hardware References](REFERENCES.md).

Configure rules with these existing flags, environment variables, or Helm
values:

| CLI flag | Environment variable | Helm value |
| --- | --- | --- |
| `--tink-controller-reference-allow-list-rules` | `TINKERBELL_TINK_CONTROLLER_REFERENCE_ALLOW_LIST_RULES` | `deployment.envs.tinkController.referenceAllowListRules` |
| `--tink-controller-reference-deny-list-rules` | `TINKERBELL_TINK_CONTROLLER_REFERENCE_DENY_LIST_RULES` | `deployment.envs.tinkController.referenceDenyListRules` |

For example, allow a single Secret reference:

```sh
export TINKERBELL_TINK_CONTROLLER_REFERENCE_ALLOW_LIST_RULES='{"reference":{"resource":["secrets"],"namespace":["tinkerbell"],"name":["workflow-credentials"]}}'
```

The policy's defaults and precedence are important:

- With no deny rules configured, Tinkerbell uses an implicit deny-all rule.
  A reference is then readable only if an allow rule matches it.
- If deny rules are configured, a reference is denied when it matches a deny
  rule, unless an allow rule also matches. A reference that matches no deny
  rule is permitted. Configure a catch-all deny rule if you want deny-by-default
  behavior with explicit exceptions.
- A matching allow rule takes precedence over a matching deny rule.
- A permitted reference still has to be readable by the Tinkerbell Service
  Account. Policy does not grant Kubernetes permissions.

Rule syntax, examples, and configuration details are in
[Hardware References](REFERENCES.md).

The Quamina rules are application-level access policy. The Tinkerbell Service
Account must also have Kubernetes RBAC permission to `get` the referenced
resources. For rendered Hardware, the backend watches reference metadata to
notice changes, so grant `get`, `list`, and `watch` for each referenced resource
using the chart's existing additional role rules:

```yaml
rbac:
  additionalRoleRules:
    - apiGroups: [""]
      resources: ["configmaps"]
      verbs: ["get", "list", "watch"]
```

Use the referenced resource's API group in `apiGroups`; core resources such as
ConfigMaps and Secrets use an empty group. RBAC grants Kubernetes access but
does not replace the Quamina policy.

## Refresh rendered Hardware

You do not need to flush a cache or restart Tinkerbell when a referenced
object's data changes. Update that Kubernetes object normally, for example with
`kubectl apply` or `kubectl edit`. The backend's metadata-only informer sees the
object update and queues each Hardware that references it. The Kubernetes
backend then reads the stored Hardware, fetches the reference's current data
from the API server, checks the reference policy, renders the Hardware's
templates, and replaces the shared in-memory result. The
Hardware Controller does not render; when enabled, it reports the backend's
render outcome in the `Rendered` condition.

The render store is separate from both the Kubernetes objects and the informer
cache. It holds one rendered Hardware object in memory and serves the same
result to components such as Smee and Tootles. Referenced-object contents are fetched live during
rendering; the informers watch metadata only to detect changes. Each Tinkerbell
process has its own render store, so replicas refresh independently. The
Hardware `Rendered` condition is reported by the leader's store; it is not an
acknowledgment that every replica has completed its refresh.

This refresh is asynchronous. Until the new render completes, a previously
successful result continues to be served. If the new render fails, that last
successful result remains in use; a Hardware that has never rendered
successfully is unavailable to Smee and Tootles until it succeeds. A process
restart clears these in-memory results, after which Hardware is rendered again.

> [!NOTE]
> Changing Hardware or a reference while a machine is booting can make that
> boot observe values from different render generations; avoid edits during
> provisioning.

When the Hardware Controller is running, `status.lastRenderTime` records when
the leader's backend completed its latest render attempt for this Hardware.
It advances after successful and failed attempts. Check it together with the
`Rendered` condition to tell when a refresh completed and whether it succeeded:

```sh
kubectl get hardware node-01 -n tinkerbell -o yaml
```

`Rendered.LastTransitionTime` still changes only when the condition status
changes, and `ObservedGeneration` tracks the Hardware generation, not
referenced-object versions. `lastRenderTime` reflects the leader's store only;
it does not acknowledge completion by every replica. To verify the actual
values being served, inspect the relevant Smee or Tootles output; the stored
Hardware itself continues to contain the template, not the rendered value.

Workflow templates read the shared rendered Hardware snapshot when the Tink
Controller processes a Workflow. A referenced-object update queues the shared
Hardware for rendering; an already-processed Workflow is not rerendered. To use
updated Hardware values, create a new Workflow after `lastRenderTime` advances
and `Rendered` is `True`. If a Workflow is processed while an update is still
rendering or the latest attempt failed, the last successful snapshot may be
used. A failed initial render is retried after the Hardware render becomes
available. Do not clear or edit controller-owned Workflow status to force a
rerender.

## Check render and policy errors

Inspect the Hardware status when the Hardware Controller is enabled and
templating is on:

```sh
kubectl get hardware node-01 -n tinkerbell -o yaml
```

Look under `status.conditions` for the `Rendered` condition. A denied reference
used by a template is reported with reason `ReferenceDenied`; a missing object
uses `ReferenceNotFound`. The message identifies the error and whether a
previous rendering is still being served. An unused denied reference does not
necessarily fail rendering or produce a `False` condition.

For Workflow rendering, inspect its status:

```sh
kubectl get workflow provision-node-01 -n tinkerbell -o yaml
```

Look at `status.templateRendering` and the `TemplateRenderedSuccess` entry in
`status.conditions`. On an initial-render failure, the condition message
contains the hardware or template rendering error. The controller retries the
failed initial processing; changing the source object or policy does not
require manually editing the Workflow status.

To resolve a denial, check that the allow rule matches the Hardware source and
reference coordinates, then check that the Service Account has the required
RBAC verbs. If the object is reported missing, verify its group,
version, plural resource, namespace, and name. Tinkerbell does not create a
separate policy-violation resource. The status conditions are the user-visible
record when their respective controllers are running; backend logs contain
render errors, and verbose logs include individual reference decisions. For
call paths and the limits of this policy boundary, see the
[developer explanation](HARDWARE_TEMPLATING_DEVELOPMENT.md).