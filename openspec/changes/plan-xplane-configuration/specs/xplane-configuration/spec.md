## ADDED Requirements

### Requirement: Select xplane configuration from the host plan
For a pool referencing DMSPlanParams, the host-flow controller SHALL resolve the same-namespace plan and structured blueprint, compile the host plan using dms-lib, and require the selected `spcx-doca-xplane-boot` TOML service artifact.

#### Scenario: Valid artifact
The controller writes the exact selected bytes to the pool's local host file before reconciling the existing xplane DaemonSet with the config file argument and mount.

#### Scenario: Invalid plan or artifact
A missing reference, selector mismatch, unsupported schema/media type, empty content or failed host write returns an error before the shared workload changes.

### Requirement: Preserve existing service lifecycle
Pools without a plan reference SHALL retain their existing xplane DaemonSet arguments and OVS preparation. A referenced pool SHALL continue to use the existing switchdev gate, OVS bridges and DaemonSet owner. Updating the host file SHALL NOT claim live reload or initiate a new rollout protocol.

#### Scenario: Updated artifact
Reconciliation atomically updates the stable host file. The running pod template remains unchanged; a new pod with a fresh file mount reads the current file.

#### Scenario: Blueprint topology authority
The controller passes the selected TOML unchanged. If the authored artifact omits interfaces, xplane can retain OVSDB topology selection; if it supplies interfaces, xplane uses that authored topology.
