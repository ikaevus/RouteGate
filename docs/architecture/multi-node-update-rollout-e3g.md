# RG-96E3g: administrator-reachable rollout API boundary

Status: design review

## Purpose

Make the already-proven E3b-E3f rolling-update backend reachable to an authenticated RouteGate administrator without adding a scheduler, automatic retry loop, a second privileged mutation path, or caller-controlled host mutation selectors.

E3g is an HTTP/API boundary only. It must compose the existing planner, durable snapshot persistence, and one-step controller rather than reproduce their SQL or mutation logic.

The intended operator model remains explicit and bounded:

1. create an immutable rollout plan for an ordered set of VPN servers and one canonical RouteGate target version;
2. inspect the durable rollout and entry state;
3. explicitly invoke one rollout step;
4. inspect the durable result before any later step.

A single HTTP request must never loop until completion and must never advance more than the one durable transition already permitted by E3f.

## Existing boundaries remain authoritative

E3g must preserve these ownership boundaries:

- E3b owns fail-closed rollout eligibility planning;
- E3c owns immutable durable rollout/snapshot persistence;
- E3d owns at-most-one single-node mutation admission;
- E3e owns post-update health proof and stop/advance eligibility;
- E3f owns one-step orchestration and the rule that one invocation cannot both prove one node healthy and admit the next node;
- E2j/E2i remain the only path from a single-node version-only job to privileged Agent-side host mutation.

No HTTP handler may create a platform-update job directly, bind a rollout entry directly, mark an entry healthy, or synthesize rollout terminal state.

## Proposed Admin API

### Create rollout

`POST /api/v1/platform-update-rollouts`

Strict JSON request:

```json
{
  "targetVersion": "v1.2.3",
  "serverIds": [
    "550e8400-e29b-41d4-a716-446655440001",
    "550e8400-e29b-41d4-a716-446655440002"
  ]
}
```

The request may select only:

- one canonical RouteGate release version accepted by the existing version-only update contract;
- an ordered list of canonical RouteGate server UUIDs.

The handler must reject unknown JSON fields, trailing JSON values, empty target versions, malformed/non-canonical UUIDs, an empty server list, and duplicate server IDs before repository/database casts.

The handler passes the ordered server IDs through the existing E3b planner and persists only the resulting E3c immutable snapshot. It must not silently drop ineligible nodes: bounded planning blockers/skips remain part of durable rollout evidence exactly as defined by E3b/E3c.

The handler must not accept Agent IDs, job IDs, roles, URLs, repositories, artifacts, checksums, filesystem paths, commands, updater arguments, environment variables, signer/trust-root selectors, maintenance windows, parallelism, retry policy, or rollback policy.

A successful creation returns `201 Created` with the canonical rollout ID and durable snapshot/status. Creation itself does not admit a host mutation.

### Read rollout

`GET /api/v1/platform-update-rollouts/{rollout_id}`

Returns the durable rollout plus ordered entries needed for operator decisions and later E4 UI rendering. The response may expose:

- rollout ID, target version, status, created/started/completed timestamps;
- ordered server IDs and entry status;
- bounded planning blocker/error codes already persisted by the rollout model;
- bound single-node job ID only after E3d has durably bound it;
- bounded rollout/update result state already present in Manager-owned durable data.

It must not expose Agent credentials/tokens, local paths, commands, raw updater output, trust material, or unbounded host error text.

Malformed rollout IDs are rejected before PostgreSQL UUID casts. Missing rollouts are returned as not found without revealing unrelated server/job existence.

### Advance exactly one step

`POST /api/v1/platform-update-rollouts/{rollout_id}/advance`

The request body is empty. No selector or override field is accepted.

The handler invokes `AdvancePlatformUpdateRollout(ctx, rolloutID)` exactly once and maps the bounded E3f result to an Admin API response. It must not call E3d/E3e directly, loop, sleep, poll, retry, or invoke E3f a second time in the same request.

The response may report only the E3f bounded result vocabulary: rollout status, `mutation_admitted`, `mutation_in_progress`, `waiting_health`, `node_healthy`, `rollout_succeeded`, `rollout_failed`, `outcome_unknown`, or `no_change`, plus Manager-owned server/job identity and bounded waiting reason when E3f provides them.

A `node_healthy` response must end that HTTP request. The next VPN node may be admitted only by a later explicit Admin request after the healthy transition is already durable.

## Authentication, authorization, CSRF and audit

All three routes are Admin-only and must use the existing RouteGate administrator authentication/authorization boundary. E3g must not create a second auth model or trust an Agent bearer credential at an Admin route.

Browser-reachable mutating routes must preserve the existing Manager CSRF/origin protections used by other privileged Admin mutations. If the current Admin API uses a single canonical protection mechanism, E3g reuses it rather than inventing an update-specific exception.

Create and advance attempts must be auditable. Audit records should contain only bounded Manager-owned identifiers/result codes such as rollout ID, target version, ordered server IDs or count where appropriate, E3f action, result and bounded failure reason. Do not log Agent tokens, authorization headers, artifacts, local paths, raw host stderr/stdout, or trust material.

## HTTP error and ambiguity semantics

HTTP transport convenience must never weaken the durable update model.

- input/identifier validation failures are bounded client errors and perform no mutation;
- planning eligibility failures remain bounded planner/snapshot outcomes rather than arbitrary SQL errors;
- E3f bounded terminal/waiting/no-change results are successful observations of durable rollout state;
- PostgreSQL, context, transaction, commit or other infrastructure errors remain errors and must not be converted into a guessed success by an HTTP re-read;
- an HTTP disconnect or timeout after an advance call begins is not permission to retry inside the handler;
- clients recover from ambiguous transport outcomes by reading the durable rollout state and may make a later explicit advance request only through the same E3f replay-safe boundary.

The API must not provide an idempotency key that can create a replacement job or bypass E3d replay identity. Durable rollout/job identity remains the only execution authority.

## Concurrency contract

Concurrent Admin `advance` requests are permitted only because E3f/E3d/E3e already serialize the authoritative durable transition. E3g adds no new cross-request lock hierarchy and holds no handler-owned database transaction across E3f.

Tests must prove that concurrent advance requests cannot:

- create two single-node mutation jobs;
- bind two rollout entries;
- admit a next node before the prior node is durably healthy;
- bypass failed/outcome_unknown stops;
- reinterpret an infrastructure/commit error as a terminal success;
- manufacture server/job identity from request data.

## Next Action First UX contract

E3g intentionally exposes one explicit `advance` action instead of an automatic execution loop. This preserves a simple future E4 UI model:

- show current rollout state;
- show the next safe action;
- let the administrator explicitly continue one step;
- after mutation admission, show that the current node is updating/waiting for health;
- after health proof, show that the node is healthy and a later explicit action may continue the rollout;
- on `failed` or `outcome_unknown`, stop and show the durable blocker without offering automatic retry.

This is an MVP operations boundary, not the later policy/scheduler layer.

## Not in E3g

E3g does not add:

- background scheduling or repeated Manager-side advancement;
- auto-update policy or release discovery;
- release channels;
- maintenance windows;
- canary groups;
- configurable parallelism;
- retry, force, cancel, rollback or resume overrides;
- automatic VPN-core updates;
- Management/Hybrid-node participation in the VPN-only rollout;
- a new privileged Agent/updater command surface;
- E4 Web UI implementation.

Those remain separate later slices.

## Validation gate

Before merge, E3g implementation must have focused tests proving at least:

- strict create request decoding and canonical identifier validation before database casts;
- ordered server selection reaches E3b/E3c without caller-selected Agent/job/mutation selectors;
- creation persists a rollout but does not create/admit a platform-update job;
- GET returns only the intended durable rollout/entry fields and is rollout-scoped;
- advance accepts only a canonical rollout ID and an empty body;
- one HTTP advance invokes E3f exactly once;
- `node_healthy` never admits the next node in the same request;
- concurrent advance requests retain the one-job/one-updating-entry invariant;
- failed/outcome_unknown are terminal operator stops;
- infrastructure/commit/context errors are not normalized into guessed success;
- malformed IDs never become SQLSTATE `22P02` / HTTP 500 paths;
- existing Admin auth/authorization/CSRF and audit boundaries are preserved;
- direct E2j single-node behavior remains unchanged;
- no new privileged selector or host-mutation path is introduced.

The design is intentionally review-first. Mutation-capable HTTP code should land only after a focused security/correctness review of this boundary. Exact-head CI plus another focused review are required before merge.
