<!-- Copyright 2026 NVIDIA CORPORATION & AFFILIATES -->

# DMS plan bindings

Spectrum-X Operator resolves `DMSPlanParams` on each host using dms-lib. A referenced
ConfigMap contains the complete structured doSPCX blueprint in
`data["blueprint.yaml"]`; it is not a base64 tar archive. The ConfigMap and
DMSPlanParams must share a namespace.

```yaml
apiVersion: dms.nvidia.com/v1alpha1
kind: DMSPlanParams
metadata:
  name: spectrum-x
  namespace: spectrum-x-operator
spec:
  nodeSelector:
    network.nvidia.com/spectrum-x: "true"
  blueprintRef: spectrum-x-blueprint
  profile: SPX_Multiplane
  platformType: gb300
  planes: 4
  spxgs: false
```

Use a blueprint authored for the actual platform, profile and observed device
layout. Profile/platform names and allowed values come from that blueprint.
Additional `parameters` retain their string values until the compiler converts
them using the selected parameter declarations. Use the explicit `planes`,
`overlay` and `spxgs` fields instead of the reserved parameter keys `planes`,
`overlay` and `SP_XGS`. The host environment is `host-k8s`.

Set `spec.planParamsRef: spectrum-x` on a same-namespace
`SpectrumXRailPoolConfig` to use these bindings. Every currently selected rail
pool node must also match the plan selector and have current bindings. A missing,
unresolved or stale binding keeps reconciliation InProgress before networking
changes. Pools without a reference retain their existing behavior.

Each host publishes its full target map in `status.targetMapByHost`, together with
its Kubernetes Node UID, plan generation, SHA-256 digest of the exact blueprint
bytes, inventory stage, and a Resolved condition. Resolved describes binding
validity; it does not mean NIC configuration, firmware activation, CC or xplane
services are ready. Existing networking and service gates remain authoritative.
This change does not configure NICs or change CC/xplane process ownership. ARC's
future configuration and readiness contract is not inferred from systemd units.

The controller polls discovery every minute and watches plan, ConfigMap and Node
changes. Incomplete enumeration or a discovery issue affecting selected hardware
preserves a previous map but marks it unresolved. An issue outside the sole
explicit BDF-bound target map can be ignored; unknown issue identities,
fingerprint-only selection and alternative candidate maps fail closed because a
missing device could change the selection. Consumers must check the condition, UID, generation and current
ConfigMap digest before using it. Projected post-breakout endpoints are not proof
that the hardware has been activated. Activation and rediscovery belong to the
configuration consumer.

Status patches use resource-version conflict retries and re-read current nodes
and inputs. Every running flows daemon also removes entries for deleted,
recreated or deselected nodes, even if its own node is no longer selected. The
flows DaemonSet is not restricted by plan selectors; this permits cleanup when a
plan selects zero nodes. No reconciliation occurs while all flows daemons are
unavailable.

Publication has a conservative 1 MiB limit for the whole serialized object,
including existing metadata. If a new map exceeds it, the controller preserves
the previous map with Resolved=False. Split the node selector across plan objects
before reaching this limit; maps are never truncated.

## Build and deployment

`make manifests` copies the DMSPlanParams CRD from the pinned dms-lib API module,
so installers include its exact wire schema. Install the generated CRD and RBAC
alongside the updated flows controller. The controller needs compatible DOCA
HOST/dmspe and YANG schemas in its image, plus host `/sys` and `/dev` access.
Device discovery failures remain visible in status and logs; they do not delete
previous inventory entries. Qualify the exact DOCA image/backend version on the
managed hardware before enabling references.

Both Go modules are pinned to published upstream revisions. While dms-lib is
private, building requires authenticated Git access and
`GOPRIVATE=github.com/Mellanox/dms-lib`. Do not replace those pins with sibling
filesystem paths; Docker build contexts cannot resolve them. Repository CI and
image builders need credentials with read access to that upstream dependency.
Dockerfiles accept an optional BuildKit secret named `netrc`, mounted only for
dependency download. Supply a GitHub-authenticated netrc file with
`docker buildx build --platform=linux/amd64 --secret id=netrc,src=/path/to/netrc ...`.
Credentials are not copied into the image or passed as build arguments.

Controller tests use envtest and real library compilation with a synthetic
blueprint; host discovery is injected. They cover concurrent publication, stale
input rejection, partial-discovery retention, node incarnation and selector
changes, publication size, parameter coercion and rail-pool gating. These tests
do not qualify physical NIC activation or ARC service compatibility.
