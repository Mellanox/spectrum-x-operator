## Context

The host-binding controller already calls BuildFromInventory with structured ConfigMap input. The library revision replaces embedded operation compilation with the external DMS CLI without changing that public call.

## Decision

Update only the library pin and necessary test compatibility. Keep the API pin because its files are unchanged between revisions. Tests add the upstream blueprint-action YANG fixture and assert CLI-projected JSON numbers without rounding large integers; diagnostic boolean values follow the planner's text representation. Production consumers do not inspect that diagnostic parameter map.

A consumer-side planner adapter would duplicate library ownership and is unnecessary. Changing runtime controller selection or xplane service orchestration would expand scope. Read-only hardware validation therefore uses a separate test manager registering only the existing DMSPlanParams reconciler; production gains no test flag or alternate flow.

## Validation boundary

Use real stock DOCA CLI/planner execution for controller cases and public planner integration cases. Run Linux build, lint and generation checks. A complete operator image build establishes packaging, while the isolated reconciler image supports separate host-binding qualification without registering the host-flow controller. Neither image build establishes NIC or xplane runtime readiness.

## CI runtime provisioning

Move the complete existing unit-test target into a repository-local job using the immutable tested DOCA 3.6 image. Use the repository's existing NVCR credentials and an environment-backed Git credential helper for private module reads. Bootstrap build tools from Ubuntu repositories, then use checkout/setup-go and the unchanged envtest/Makefile command. Preserve reusable build, lint and generation checks and the existing coverage upload. Go-only test runners cannot execute the newly external planner dependency; skipping those cases or substituting a fake planner would remove relevant coverage.
