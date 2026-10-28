# Design

## Context

Each flows daemon can discover its host. The shared library provides DMSPlanParams types, complete blueprint parsing, inventory-aware compilation and resolved target maps. The operator owns Kubernetes input selection and status publication.

## Goals / Non-Goals

**Goals:** publish independently resolved host bindings and make stale input/output relationships explicit.

**Non-Goals:** NIC mutation, service readiness or ownership, RailPoolConfig changes, custom blueprint compilation, status-size policy or a daemon image upgrade.

## Decisions

1. Read `data["blueprint.yaml"]` from the same-namespace ConfigMap. Map CRD fields to build options with environment `host-k8s`; pass additional string parameters to the library for schema coercion. Explicit fields own reserved parameter keys.
2. Call `BuildFromInventory` with the complete inventory and obtain `TargetMap` from the compiled plan. Keep blueprint interpretation and issue relevance inside the library.
3. Recheck live Node UID/membership, generation and exact blueprint bytes before publishing. Use optimistic status updates with conflict retries to preserve peer entries and explicit null values; return API-server errors for normal reconciliation retry.
4. Watch intent, referenced blueprints and Node membership changes, and refresh periodically. Ignore Node status-only updates. Retain last maps with False conditions after failure; remove entries for deleted, deselected or replaced Nodes.

## Risks / Trade-offs

- The required CRD watch needs CRD/RBAC installation before daemon startup. Cleanup waits if all daemons are unavailable.
- Resolved bindings do not certify NIC configuration or service readiness. Consumers must verify their own host's current metadata and condition.
- Private Go module access and unchanged image compatibility remain deployment constraints. An optional build secret only assists dependency download; it does not grant access.
- Envtest and read-only host qualification cover reconciliation, not NIC/service writes or full-image deployment. The pre-existing unrelated staleflows race is tracked in #353.
- OpenSpec validation is structural; it does not establish runtime conformance.
