## Implementation
- [x] Remove planner executable build helper and Docker packaging.
- [x] Retain established library consumer interfaces.
- [x] Pin library/API f89d1f2937ff and shared planner 54bb3fa91b4c.

## Validation
- [x] Run Linux/amd64 build, full existing tests and race tests; retain failures.
- [ ] Resolve declared boolean compatibility failure (GitLab library issue 3); seven Xplane failures remain outside this validation scope. The existing staleflows race is tracked in operator issue 353.
- [x] Run lint and generated-file checks (both pass; generation unchanged).
- [ ] Publish updated draft PR with exact results and remaining limits.
- [ ] Build exact-pin images and perform separately attributed hardware validation.
