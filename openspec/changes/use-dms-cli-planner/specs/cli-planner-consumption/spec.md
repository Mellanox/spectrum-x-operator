## ADDED Requirements

### Requirement: Consume CLI-backed plans through the existing library interface

The operator SHALL use the published CLI-planner library revision through its existing structured-blueprint and inventory inputs without adding consumer-side planning or service orchestration.

#### Scenario: Resolve structured plan intent

- **WHEN** the DMSPlanParams reconciler receives selected host inventory and a structured blueprint ConfigMap
- **THEN** it obtains resolved bindings through the existing library interface and retains existing status freshness and failure handling

### Requirement: Verify the actual planner runtime boundary

Consumer validation SHALL exercise the real compatible DMS CLI and planner, include the blueprint action in its test schema, and preserve exact parameter values across the planner JSON representation.

#### Scenario: Explicit parameter values

- **WHEN** the controller forwards explicit zero, false, empty text and an integer beyond the exact IEEE-754 range
- **THEN** validation checks the resulting planner values without converting the large integer through a floating-point representation

#### Scenario: Bounded host qualification

- **WHEN** host-binding qualification excludes xplane delivery and startup
- **THEN** its test manager registers only the existing DMSPlanParams reconciler and reports image packaging separately from hardware or service readiness

### Requirement: Provision the real planner for hosted unit tests

The repository SHALL run its complete unit-test target in the immutable compatible DOCA runtime using existing registry and repository credentials, while retaining other reusable CI checks and coverage upload.

#### Scenario: Hosted controller tests call the planner

- **WHEN** CI executes controller cases that compile a structured plan
- **THEN** the installed real CLI and planner execute those cases without mocking or skipping them
