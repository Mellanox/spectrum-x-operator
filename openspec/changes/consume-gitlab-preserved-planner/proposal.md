## Why

Validate Spectrum-X Operator against the preserved upstream GitLab library and planner using published modules. The existing Go import paths can remain stable while the build resolves all artifacts from one reviewed GitLab revision.

## What Changes

- Replace the root and shared API modules with versioned GitLab modules and copy the generated API schema, including naming and physical port fields.
- Build the preserved planner module at the root library replacement's exact version and install it at the library's required executable path.
- Document the supported planning boundary and retain existing feature assertions during validation.

## Capabilities

### New Capabilities

- `gitlab-planner-packaging`: package the matching preserved planner alongside the versioned GitLab library.

### Modified Capabilities

None.

## Impact

The dependency requires Go 1.26.3; the operator Docker builder remains Go 1.27. Private module downloads require approved repository access through an ephemeral BuildKit netrc secret. No new CI secret is assumed. The preserved library currently supports planning and target projection; discovery, service payloads and hardware/lifecycle ownership remain unsupported. Full-suite failures must retain those limits. Xplane generation, delivery and startup are outside the requested qualification.
