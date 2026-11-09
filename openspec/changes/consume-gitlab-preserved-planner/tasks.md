## 1. Wire published dependencies and planner packaging

- [x] 1.1 Pin versioned GitLab root/API replacements while retaining imports and the required Go minimum.
- [x] 1.2 Build the matching preserved planner using the same guarded helper as the other consumer.
- [x] 1.3 Copy the shared generated CRD and verify naming/physical port schema fields.

## 2. Validate the bounded migration

- [x] 2.1 Run Linux build, unchanged full unit suite, lint and generation checks; retain unsupported feature failures.
- [x] 2.2 Prepare the opt-in caller-owned status test outside product commits and compile its test-only image assets.
- [ ] 2.3 Validate strict change structure and review the exact diff before committing or opening a PR.
- [ ] 2.4 Build and publish the authorized Linux/amd64 image with module, planner and image identity evidence.

Cluster deployment and captured-inventory qualification are coordinator-owned. Neither image generation nor an injected inventory establishes live discovery, NIC mutation or xplane readiness.

Local results: Linux build, lint (zero issues), vet and generation pass. The unchanged controller suite runs all 146 cases: 137 pass and 9 fail (seven unsupported service-payload cases, one conservative discovery-issue rejection, and one diagnostic parameter-type assertion). No existing assertion was relaxed. The opt-in external test source compiles; in-cluster execution and test-image approval remain separate.
