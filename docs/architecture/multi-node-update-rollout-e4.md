# RG-96E4: administrator rollout presentation

Status: implemented

## Purpose

RG-96E4 makes the durable E3 rollout contract usable from the RouteGate Settings page without adding a browser-side scheduler, retry loop, privileged selector, or second orchestration implementation.

The UI remains a presentation adapter over the three E3g endpoints:

- create one immutable rollout snapshot;
- read one known rollout by ID;
- explicitly advance that rollout by exactly one controller step.

The Manager remains authoritative for eligibility, durable state, admission, health proof, and terminal outcomes.

## Creation contract

The creation form deliberately exposes only the values already accepted by E3g:

- the target is locked to the canonical version currently reported by the Manager;
- only inventory entries whose deployment role is exactly `vpn` are selectable;
- selected server IDs are visibly ordered and may be moved before creation;
- the UI enforces the 1,024-member API bound;
- creation records a plan and does not advance or admit a node update.

The client stores the exact target version, ordered server IDs, and a generated canonical UUIDv4 idempotency key before sending the create request. If the response is lost, the recovery action resends the exact request with the same key. The form cannot edit that stored attempt. Discarding the local recovery record requires an explicit warning because it cannot cancel a rollout that may already exist on the Manager.

Browser storage is recovery metadata, not rollout authority. Failure to persist the create attempt blocks the request. After successful creation, the durable rollout ID is shown and retained only as a convenience pointer.

## Operator progression

The active view renders Manager-owned rollout and ordered entry state, including bounded planning blockers, job IDs, timestamps, waiting reasons, and terminal reason codes.

Progression follows these rules:

1. one button activation sends one `POST .../advance` request;
2. synchronous in-flight guards suppress rapid duplicate create/advance events, and the client library and mutation layer disable automatic retry;
3. a successful response is followed by a GET of durable state;
4. a transport or server failure is treated as ambiguous and disables further advance actions;
5. the administrator must complete a successful GET refresh before another explicit step becomes available;
6. `failed` and `outcome_unknown` are terminal displays with no retry or force action;
7. proving one node healthy never admits the next node in the same UI action.

The browser does not poll or auto-advance rollout state. The administrator explicitly refreshes or advances. The existing server inventory refresh is read-only and does not affect the immutable rollout snapshot.

## Resume and history boundary

E3g intentionally does not expose a fleet-wide rollout-list endpoint. E4 therefore supports resuming one known canonical rollout UUID and remembers the currently viewed ID in local browser storage. Losing that pointer does not lose rollout state; it remains durable on the Manager and can be loaded again when its ID is known.

Adding search, listing, cancellation, force, or history management would require a separately reviewed API contract.

## Localization and presentation

The feature is English-first with complete Russian localization. Static operator-facing text is routed through the shared typed i18n dictionary. Unknown future bounded status/reason codes remain visible in their raw form rather than being hidden or guessed.

The panel is part of Settings and is visually separate from the local Management/Hybrid update workflow. This preserves the operational order: update and verify the Manager first, then create a VPN-node rollout to that exact Manager version.

## Explicit non-goals

E4 does not add:

- release selection, arbitrary versions, URLs, artifacts, repositories, signers, or trust roots;
- Agent IDs, job IDs, commands, paths, updater arguments, or other privileged selectors;
- background scheduling, maintenance windows, canaries, configurable parallelism, or automatic progression;
- automatic create/advance retry, terminal retry, force, cancel, rollback, or mutation reconciliation;
- Management or Hybrid nodes in the VPN rollout;
- a new backend endpoint or database state.

Release channels and controlled unattended policy remain a separate RG-96F evolution.

## Validation gate

The E4 change is gated by:

- an executable Node safety-model test covering terminal stops, exact ordered request identity, bounded stored recovery attempts, and next-action selection;
- the existing frontend localization check;
- strict TypeScript and production Vite build;
- exact-head repository CI and focused review before merge.
