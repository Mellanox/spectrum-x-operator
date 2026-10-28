# Tasks

## 1. Implement host binding reconciliation

- [x] 1.1 Wire shared CRD types, library build options and structured blueprint loading.
- [x] 1.2 Publish independent host results with input rechecks, conflict handling, stale-entry cleanup and explicit-null preservation.
- [x] 1.3 Add relevant watches, periodic refresh, RBAC and CRD packaging without changing RailPoolConfig or service flows.

## 2. Verify and document the boundary

- [x] 2.1 Cover parameter mapping, invalid inputs, concurrent status updates, stale identities, membership and null preservation with controller envtests.
- [x] 2.2 Exercise actual read-only reconciliation on both mevo-carmel-1 workers and document that no NIC/service writes or full-image deployment were tested.
- [x] 2.3 Document CRD installation, private dependency access and the unchanged image compatibility boundary.
- [x] 2.4 Record standard OpenSpec artifacts and validate their structure.

Completed implementation tests do not assert that hosted CI access, all unrelated race tests or deployment qualification passed. The status-size policy removal was checked with the controller race suite and corrected Linux lint run.
