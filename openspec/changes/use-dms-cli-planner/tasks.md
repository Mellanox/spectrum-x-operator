## 1. Update the dependency and test compatibility

- [x] 1.1 Pin the published library revision without a local module replacement or API-module change.
- [x] 1.2 Add the blueprint plan action to the test schema and preserve exact CLI parameter-value coverage.
- [x] 1.3 Keep production controller and service ownership unchanged.

## 2. Validate and package

- [x] 2.1 Verify the stock DOCA 3.6.0082-1 runtime with all 12 public planner integration cases.
- [x] 2.2 Pass the complete controller suite and remaining consumer unit suites with the real stock runtime.
- [x] 2.3 Pass Linux lint, generation, build and strict OpenSpec validation.
- [ ] 2.4 Build and publish the Linux/amd64 operator image and separate test-only reconciler image with exact source and digest evidence.

Cluster qualification is performed separately. Xplane configuration generation, delivery and startup are excluded from hardware qualification; its existing controller unit coverage still runs.

Local validation: 146 controller cases passed with stock DOCA 3.6.0082-1; remaining unit suites passed; Linux Go 1.26.6 lint reported zero issues, generation had no tracked drift, and vet passed. This change passes strict OpenSpec validation. Repository-wide strict validation retains pre-existing scenario-format warnings in plan-xplane-configuration. Image identities and per-step logs are retained outside the repository.

## 3. Provision the hosted test runtime

- [x] 3.1 Route the complete existing unit-test target through the immutable tested DOCA container, preserving reusable build/lint/validate jobs and coverage upload.
- [x] 3.2 Validate workflow syntax and the container bootstrap/test command locally; distinguish hosted credential access and CI results from local checks.

The final published library pin also passes the complete unchanged `make unit-test` target in the immutable DOCA runtime with Go 1.26.6, plus build/lint/generation/vet. Workflow actionlint and strict change validation pass. Hosted secret permissions and hosted run results are verified separately from these local checks; existing unrelated reusable-job access failures remain visible.
