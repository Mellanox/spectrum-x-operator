## ADDED Requirements

### Requirement: Package one reviewed GitLab revision

The build SHALL resolve the root library and shared API through real versioned GitLab replacements, and derive the preserved planner module version from that same root replacement.

#### Scenario: Matching published modules

- **WHEN** the root and API replacements identify the same reviewed GitLab version
- **THEN** the build downloads the preserved doSpcx module at that version and builds its planner entrypoint

#### Scenario: Inconsistent replacement

- **WHEN** the library path is unexpected or the API replacement has a different version
- **THEN** the helper fails before packaging a mismatched planner

### Requirement: Use the packaged planner explicitly

The runtime SHALL contain the built planner at /usr/local/libexec/dms-blueprints-lib/planner without relying on an installed DOCA planner or sibling source directory.

#### Scenario: Packaged executable provenance

- **WHEN** a Linux/amd64 image is produced
- **THEN** its library and API replacements, planner download hash and installed executable hash identify the reviewed source and architecture

### Requirement: Preserve capability limits during qualification

Validation SHALL retain existing feature assertions and distinguish unsupported operations, captured-inventory planning, image packaging and live hardware readiness.

#### Scenario: Unsupported discovery

- **WHEN** the real reconciler calls this library's discovery implementation
- **THEN** it publishes unresolved status from the genuine unsupported error rather than claiming discovered bindings

#### Scenario: Captured inventory and synthetic failure

- **WHEN** the opt-in test injects a caller-provided captured inventory and then a synthetic selected-ASIC product mismatch
- **THEN** actual planner results drive attributed status success, failure and recovery without service or production input writes
