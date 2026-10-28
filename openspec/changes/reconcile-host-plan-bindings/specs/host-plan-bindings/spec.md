# Host Plan Bindings

## Purpose

Define how Spectrum-X Operator publishes resolved per-host DMS target maps from shared plan intent and observed inventory.

## ADDED Requirements

### Requirement: Compile structured intent through the library

The controller SHALL load the referenced same-namespace ConfigMap's `blueprint.yaml`, map DMSPlanParams fields to library options and delegate inventory checks, parameter coercion and compilation to dms-lib.

#### Scenario: Explicit planner values

- **WHEN** a selected node has a blueprint and planner parameters
- **THEN** the controller preserves explicit values such as `spxgs: false`, rejects reserved-key collisions and obtains the target map from the compiled library plan

### Requirement: Publish current independent host results

Each host entry SHALL identify its Node UID, plan generation, exact blueprint-byte SHA256 digest, observed inventory stage and Resolved condition. Status updates SHALL preserve other hosts' entries and explicit nulls. Resolved SHALL describe bindings only, not NIC or service readiness.

#### Scenario: Concurrent host publication

- **WHEN** two daemons publish results concurrently
- **THEN** optimistic conflict retries retain both valid host entries without converting explicit null values into deletions

#### Scenario: Inputs change during reconciliation

- **WHEN** the Node identity or membership, plan generation or blueprint content changes before publication
- **THEN** the old computation is not published as current

#### Scenario: Failed refresh

- **WHEN** discovery or compilation fails after a prior successful map
- **THEN** the old map is retained for diagnosis with Resolved=False and is not represented as a current binding

### Requirement: Reconcile membership and aggregate state

The controller SHALL refresh selected hosts periodically, react to relevant input changes and remove stale entries for deleted, deselected or replaced Nodes. Aggregate Resolved SHALL be true only when every matching host has current valid bindings. API publication failures SHALL use normal reconciliation error handling.

#### Scenario: No matching nodes

- **WHEN** a selector no longer matches any node
- **THEN** stale entries are removed and aggregate status reports NoMatchingNodes

#### Scenario: Node status-only change

- **WHEN** only a Node's status changes
- **THEN** that update does not trigger a new inventory scan

#### Scenario: Publication error

- **WHEN** the API server rejects a status update
- **THEN** the controller returns the error for normal retry without substituting a custom size-budget result
