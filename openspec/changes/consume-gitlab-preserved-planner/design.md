# Design

Keep existing library imports with exact versioned GitLab replacements. Go does
not inherit dependency replacements, so the consumer also pins the canonical
`nvidia.com/doca/dms/doSpcx` module to the library's published source revision.
The core planner is linked into the operator. No runtime planner executable or
build-dms-planner helper is needed. Existing DMS commands still use dms-cli.

Controller calls retain the established plan API. No controller or test
assertions change. Upstream host-k8s planning does not return rendered service
payloads: seven existing Xplane assertions remain failing, outside the requested
hardware validation scope. The string `"false"` for a declared bool also remains
a string, unlike the former compiler; GitLab library issue 3 tracks this confirmed
compatibility gap. Full compatibility is not claimed while it remains unresolved.

Authenticated private GitLab access is required. Clean-container anonymous
repository and module retrieval fails; public CI needs supported credentials or
an authorized access change. This change does not alter project visibility.

Validate build, existing tests, race tests, lint and generation separately from
image publication and hardware qualification. Previous subprocess/stub-based
image results do not qualify this version.
