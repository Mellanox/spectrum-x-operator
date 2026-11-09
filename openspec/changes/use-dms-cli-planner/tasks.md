## 1. Update the dependency and test compatibility

- [x] 1.1 Pin the published library revision without a local module replacement or API-module change.
- [x] 1.2 Add the blueprint plan action to the test schema and preserve exact CLI parameter-value coverage.
- [x] 1.3 Keep production controller and service ownership unchanged.

## 2. Validate and package

- [x] 2.1 Verify the stock DOCA 3.6.0082-1 runtime with all 12 public planner integration cases.
- [x] 2.2 Pass the complete controller suite and remaining consumer unit suites with the real stock runtime.
- [x] 2.3 Pass Linux lint, generation, build and strict OpenSpec validation.
- [x] 2.4 Build and publish the Linux/amd64 operator image and separate test-only reconciler image with exact source and digest evidence.

Cluster qualification is performed separately. Xplane configuration generation, delivery and startup are excluded from hardware qualification; its existing controller unit coverage still runs.

Local validation: 146 controller cases passed with stock DOCA 3.6.0082-1; remaining unit suites passed; Linux Go 1.26.6 lint reported zero issues, generation had no tracked drift, and vet passed. This change passes strict OpenSpec validation. Repository-wide strict validation retains pre-existing scenario-format warnings in plan-xplane-configuration. Image identities and per-step logs are retained outside the repository.
