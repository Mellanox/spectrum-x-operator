## Decision

Retain the established github.com/Mellanox/dms-lib imports and use real versioned replacements for GitLab .git/lib and .git/lib/api. A small shared build helper checks the exact library replacement path and matching API version, downloads .git/doSpcx at that same version, and builds its ./planner entrypoint with readonly module files. Docker installs the resulting binary at /usr/local/libexec/dms-blueprints-lib/planner. The library has no PATH or installed-DMS planner fallback.

Building from a sibling checkout would not survive an isolated Docker context. Using the installed DOCA planner would not establish that the reviewed upstream revision was packaged. A separate independently chosen planner version would create unnecessary compatibility drift; derive it from the root replacement instead.

## Validation boundary

Use the immutable selected DOCA runtime for Linux/amd64 validation, with the original Go 1.27 operator builder. Preserve all existing regular feature assertions. Record unsupported discovery/service behavior and full-suite failures rather than replacing assertions with successful stubs. Hosted private-fetch access is a separate condition from authenticated local module downloads.

A test-only opt-in Ginkgo source is retained outside product commits and compiled through the existing controller test seam. It calls only DMSPlanParams reconciliation against caller-owned test resources. Real candidate status responses establish attribution when production controllers also watch that namespace. Captured-inventory planning is distinct from discovery, and a synthetic product mismatch verifies failure/recovery without changing production input or invoking service writers. The existing envtest suite bootstrap remains intact; deployment and cluster writes belong to the coordinator.

## Provenance

Retain module download version, checksum and Origin.Hash, plus the installed planner binary hash. A binary built by go -C inside a downloaded module can report its main module as devel; that value alone does not establish a source revision.
