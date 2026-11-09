## Why

Host-plan compilation must use the shared library revision that delegates planning to the DMS CLI. The existing consumer API remains compatible, but controller test schemas must expose the blueprint plan action and assertions must follow the CLI's JSON parameter representation.

## What Changes

- Pin the published dms-lib planner revision while retaining the unchanged shared API module.
- Extend the controller test schema with the blueprint plan action and preserve exact parameter-value assertions across CLI JSON decoding.
- Verify the existing consumer with the stock DOCA 3.6 runtime and Linux/amd64 image build.
- Move the complete hosted unit-test target into that immutable runtime while preserving other reusable CI checks and coverage.

## Capabilities

### New Capabilities

- `cli-planner-consumption`: consume CLI-backed plans through the existing library interface.

### Modified Capabilities

None.

## Impact

Production controller flow, service ownership and CRDs remain unchanged. Planning now requires an available compatible DMS CLI and planner; stock DOCA 3.6.0082-1 is the runtime tested here. This change depends on dms-blueprints-lib PR #43. Hardware qualification is separate and excludes xplane configuration generation, delivery and startup.
