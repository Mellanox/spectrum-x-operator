## Why

The xplane daemon should accept a service configuration artifact selected by a structured DMS plan while Spectrum-X Operator keeps its existing OVS and DaemonSet lifecycle.

## What Changes

- Add optional `SpectrumXRailPoolConfig.spec.dmsPlanParamsRef` in the pool namespace.
- Compile host-k8s intent through dms-lib and write the selected xplane TOML unchanged on the host.
- Start xplane with that file using the existing DaemonSet when a plan is referenced.

## Impact

Pools without the reference retain existing behavior. File changes become available on a new pod mount; no live reload or configuration-driven rollout is claimed. Blueprint authors choose whether xplane reads topology from OVSDB or from configuration. The shared DaemonSet may roll before a peer has written its file on first enablement, delaying that peer's replacement pod. The release baseline is Network Operator v26.10.0-rc.3.
