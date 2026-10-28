# DMS host plan bindings

Each flows daemon reconciles `DMSPlanParams` for its host. It discovers local devices,
compiles a plan through `dms-lib`, and publishes the resolved target map. This
controller does not configure NICs or start services. `SpectrumXRailPoolConfig`
and its reconciliation remain unchanged.

## Inputs

Store the structured blueprint in a ConfigMap's `data["blueprint.yaml"]`. It is
YAML with the library's complete blueprint structure, not a base64 tarball.
`blueprintRef` names that ConfigMap in the same namespace:

```yaml
apiVersion: dms.nvidia.com/v1alpha1
kind: DMSPlanParams
metadata:
  name: compute-plan
  namespace: spectrum-x-operator
spec:
  nodeSelector:
    node-role.kubernetes.io/worker: ""
  blueprintRef: compute-blueprint
  profile: SPX_Multiplane
  platformType: gb300
  planes: 2
  spxgs: false
  parameters: {}
```

The controller maps these fields to library build options with environment
`host-k8s`. The explicit `planes`, `overlay`, and `spxgs` fields own the planner
parameters `planes`, `overlay`, and `SP_XGS`; those keys cannot also appear in
`parameters`. The bridge preserves explicit zero and false values; CRD admission requires
`planes` to be at least one. Additional parameters remain strings until the selected blueprint's
parameter declarations coerce and validate them in the library.

The library owns blueprint loading, discovery-issue relevance, inventory stage
selection, target resolution, and preparation/runtime compilation. The controller
supplies the full inventory to `plan.BuildFromInventory` and publishes the
compiled plan's `TargetMap`. It does not duplicate the blueprint schema or infer
PF indices from PCI function numbers.

## Status and freshness

`status.targetMapByHost` is keyed by Kubernetes Node name. Each entry records the
Node UID, the plan generation, the SHA256 digest of the exact `blueprint.yaml`
bytes, the target map with its observed inventory stage, and a `Resolved`
condition. `Resolved=True` means bindings compiled successfully; it does not mean
NIC configuration or external services are ready.

On a failed refresh, the previous map remains available for diagnosis but its
host condition becomes `Resolved=False`. Consumers must require the current Node
UID, plan generation, blueprint digest, nonempty inventory stage, and a current
`Resolved=True` condition before using a map. A projected future target is not
evidence that its PCI endpoint currently exists.

The controller watches plan generations, referenced ConfigMaps, and Nodes, and
refreshes discovery every minute. Node status-only updates do not trigger
discovery; identity, label, and deletion changes do. Before publishing it rereads the plan,
blueprint, and node membership. Optimistic conflict retries merge independent
host updates without overwriting another host's result. Deleted, deselected, and
recreated Nodes lose stale entries even when the reconciling daemon's own Node
is not selected. If every daemon is unavailable, cleanup waits for a daemon to
resume.

The aggregate condition is true only when every matching host has current valid
bindings. Hosts can resolve independently while others remain unresolved. A
one-MiB publication budget rejects oversized replacements and marks the host
unresolved while retaining its prior map where possible.

## Packaging and validation

The shared CRD comes from the pinned `github.com/Mellanox/dms-lib/api` module;
`make manifests` copies its generated CRD and generates controller RBAC. Install
that CRD and RBAC before upgrading the flows daemon: its new controller watch
is required for manager startup. Local
discovery requires compatible DOCA tools and schemas, host `/sys`, and host
`/dev` for fwctl discovery. This change does not update the daemon's DOCA base
image. DOCA 3.6 qualification does not establish compatibility with every image.

The library is currently private. Dependency download and image builds need
repository read access; this change does not grant credentials or alter library
visibility. The Dockerfile accepts an optional BuildKit secret named `netrc`
for its existing dependency-download step, for example
`--secret id=netrc,src=/path/to/authorized.netrc`. The secret is not copied into
the image. Hosted CI without repository access cannot fetch the dependency.

Controller envtests exercise real Kubernetes status updates, concurrent host
publication, stale inputs, node replacement, discovery failures, parameter
preservation, and size limits. They do not establish NIC mutation, service
readiness, or hardware qualification.
