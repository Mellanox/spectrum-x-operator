## Context

The host-flow controller already prepares OVS bridges after its switchdev gate and reconciles the existing xplane DaemonSet. A referenced DMSPlanParams selects structured blueprint input; dms-lib returns the selected xplane service artifact. The release xplane image accepts `--config` at startup.

## Decision

Keep plan resolution, compilation and service selection in the existing host-flow reconciliation. Write the selected TOML unchanged to one pool-specific host path before the existing DaemonSet sync. Use its existing file mount and `--config` argument only for referenced pools. Validate selector coverage and the service schema/media type before writing. Watch referenced plan and blueprint updates so the host file is refreshed; no configuration-driven workload rollout is requested.

The DaemonSet still has one shared template, and each host supplies bytes at the same path beneath its own host filesystem. A missing file prevents xplane startup on that host. A changed file is consumed by a new pod with a fresh mount. Restarting a process within the same container may still see an older file inode. The release image has no verified live reload contract, so this change does not promise immediate activation of updated bytes. On first enablement, an unprepared peer can temporarily lose xplane availability when the shared DaemonSet rolls; its required File mount prevents starting with missing configuration.

## Behavior and limits

Existing switchdev readiness, OVS bridge creation, service ownership, and no-reference behavior remain. Authored TOML controls whether xplane obtains topology from OVSDB or from configuration; the operator does not transform it. Blueprints intended to retain OVSDB topology authority must omit configuration interfaces. There is no cross-host preparation protocol, Node annotation, new restart rule or ARC service handoff.
