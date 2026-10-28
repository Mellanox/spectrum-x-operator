# Proposal

## Why

Consumers need a shared, current mapping from DMS plan intent to each host's observed devices. Spectrum-X Operator can publish those bindings using the generic library without adding Kubernetes behavior to dms-lib.

## What Changes

- Add per-host reconciliation for the shared DMSPlanParams CRD using structured ConfigMap blueprints and dms-lib inventory/planning APIs.
- Publish host target maps with Node UID, generation, exact blueprint digest and resolution conditions; retain failed maps for diagnosis without presenting them as current.
- Wire controller watches, CRD packaging and RBAC. Keep SpectrumXRailPoolConfig and NIC/service configuration flows unchanged.

## Capabilities

### New Capabilities

- `host-plan-bindings`: resolve and publish current per-host DMS target maps.

### Modified Capabilities

None; no existing OpenSpec capability is modified.

## Impact

PR [#354](https://github.com/Mellanox/spectrum-x-operator/pull/354), implementing [#352](https://github.com/Mellanox/spectrum-x-operator/issues/352), adds a flows-daemon controller and shared-library dependencies. Install the CRD and RBAC before the upgraded daemon. Private dependency access is tracked in [#355](https://github.com/Mellanox/spectrum-x-operator/issues/355); the daemon image version is unchanged.
