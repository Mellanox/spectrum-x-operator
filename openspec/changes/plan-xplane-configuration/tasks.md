## Implementation

- [x] Add the optional pool reference, compile the selected service artifact and deliver it through the existing xplane DaemonSet.
- [x] Preserve the existing switchdev/OVS gates and no-reference behavior.
- [x] Remove Node receipts, cross-host acknowledgments, immutable bundles and revision-based rollouts.

## Verification

- [x] Validate startup delivery, selector and service failures, stable file updates and unchanged workload reconciliation.
- [x] Run Linux build, controller race suite, lint, generated manifests and strict OpenSpec.
- [x] Complete fresh council review of the final diff and update PR #356.
