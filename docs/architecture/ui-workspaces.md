# UI workspace patterns (RG-80)

## Scope

The account workspace establishes reusable interaction patterns for the admin UI.
Dashboard summaries are entry points into those workspaces. This document describes
implemented conventions; it does not introduce a separate component library.

## Navigation and hierarchy

- Use URL-backed workspace navigation for distinct domains: overview, access,
  routing, protocols, traffic and settings. Preserve list filters when navigating.
- Make the selected section visible on narrow screens. After route navigation,
  move focus to the workspace content without trapping keyboard navigation.
- Use Section for a coherent task with a heading, description and relevant actions.
  Reserve cards for meaningful entities or summaries rather than every field.
- Keep one clear primary action per task. Put secondary explanations and uncommon
  actions behind disclosure, while keeping errors and required steps visible.

## Selection and actions

Use a list and a focused inline detail view when selecting a device or protocol.
Keep the selected entity identifiable alongside its actions. Reset entity-specific
state when changing accounts; never reuse a credential or confirmation across accounts.

Native disclosure is sufficient for secondary actions today. A future Drawer or
ActionMenu must have a clear label, predictable keyboard behavior, focus restoration
and an explicit close mechanism. Neither is a new generic component in this slice.
Do not move a long, independently navigable workflow into a transient overlay.

Place rotation, revocation and deletion next to the affected entity. Explain the
consequence and require the existing confirmation before performing the mutation.
Default to concealed credentials and deliberate reveal/copy actions. Never place
secrets in navigation URLs, notifications or diagnostic output.

## Status and next action

Use actionable notices for feedback (currently form-message markup, not a shared
Notice component). A failed operation should leave the form and
its unsaved input available for correction. Background status refresh must not
replace an in-progress edit.

Distinguish loading, unavailable and a successfully loaded empty result. A failed
request must not produce a numeric zero that looks authoritative. Keep connection
confidence intact: recent activity is not proof of an exact current connection.

Dashboard links should land on the object or domain represented by the summary.
Deployment is server-scoped; select a server first or open the server shown in the
recent deployment. A ready account opens access management. Navigation must not
implicitly install, apply, rotate or revoke anything.

## Responsive and accessibility checks

Use native links and buttons, visible keyboard focus and associated form labels.
Allow navigation and dense tables to scroll within their own container where
necessary; avoid horizontal page overflow. Check mobile and desktop layouts in
both themes, as well as empty/error states and unusually long names.

## Reference implementations

- Account workspace: `frontend/src/pages/vpn-accounts/`
- Shared workspace primitives: `frontend/src/shared/ui/`
- Dashboard summaries: `frontend/src/pages/dashboard/DashboardPage.tsx`
- Guided onboarding: `frontend/src/pages/dashboard/GettingStartedWidget.tsx`

Account, server and routing-profile workspaces now implement domain navigation.
This does not claim migration of every administrative screen or completion of all
original design-system primitives. See the completion audit below.

## Integration verification

`Workspace integration` runs Chromium against the real Manager and a disposable
PostgreSQL 16 database in GitHub Actions. It covers login, Dashboard navigation,
all account workspace routes, persisted account edits, device creation and mobile
layout in both themes. Run `npm run test:workspace-integration` from `frontend`
with the same isolated environment defined in the workflow. The script requires
`ROUTEGATE_E2E_ISOLATED=1`, a loopback database named `routegate_workspace_e2e`,
`ROUTEGATE_DATABASE_URL` and `ROUTEGATE_E2E_MANAGER` (the compiled binary path).

The job creates test records and does not use a deployed installation. It does not
start an Agent, install a VPN runtime or verify real client connectivity. Those
remain separate staging checks before merging or deployment.

## Server workspace foundation

Server details now use `/servers/:serverId/:section` with overview, connection,
services, routing, deployments and settings. The old server URL and unknown
sections resolve to overview while retaining query parameters and navigation state.
The header preserves identity, role and connection state across domains.

The VPN service panel and Agent guidance are passed as React content instead of
being inserted through document-wide DOM queries. Domain panels remain mounted
while hidden to preserve drafts and in-flight operation state; switching server
identity remounts the workspace so tokens and dialogs cannot follow another server.
Shared navigation styling now belongs to WorkspaceNav itself.

Configuration versions now use a selectable list and focused actions. Deployment
history keeps failures visible and puts stages/timestamps in disclosure. The server
inventory uses five grouped columns and labelled cards on narrow screens. These
changes were deployed in PR #463 and visually accepted by the owner.

## Routing profile workspace

Routing profiles use `/routing-profiles/:profileId/:section` with overview, rules
and settings. Legacy and unknown section URLs redirect to overview, retaining
query parameters. The profile list remains alongside the selected workspace on
wide screens, with persistent profile identity and default/custom status.

The rule editor opens deliberately after the rule list. Domain switches retain
unsaved settings and rule input; changing profile identity remounts local state.
Profile refreshes do not overwrite dirty settings, including refreshes after rule
operations. Pending writes disable conflicting form controls. Deletion is scoped
to the named profile or rule and requires confirmation; default profiles remain
protected. Creating a profile opens its rules; deleting it returns to the list.
Callbacks from unmounted workspaces do not redirect the newly selected profile.

The rule list selects a focused detail card with complete matcher values and scoped
actions. Selecting a rule is read-only; selection stays fixed while the editor is
open. Saving selects the returned rule; deleting the selection falls back to the
first remaining rule. The editor groups domains and IP networks, with keywords and
GeoSite/GeoIP tags under additional matchers. Existing additional values expand that
group when editing, and closing it never removes values from the save payload.
The existing matcher fields, rule priority/action semantics and APIs are retained.
Integration checks verify all six matcher arrays survive creating and editing a rule.

Workspace integration additionally verifies a routing rule created through the UI,
settings-draft preservation during that save, persisted profile edits, routing
navigation and mobile layout against Manager and PostgreSQL.

## Completion audit — 26 September 2026

Reviewed source baseline: `3760acb5790356e758fe17155bccb777523e6647`.
Status: **in progress; not ready to close RG-80**.
PRs #462, #463, #464 and #469 delivered the workspace migrations and routing copy
polish. This audit is a source/documentation review, not a new live browser or VPN
connectivity test. Owner screenshots demonstrate desktop routing overview, empty
rules and the open editor; they do not establish all-domain mobile acceptance.

| Original requirement | Evidence and current status |
| --- | --- |
| Workspace navigation | Shared `WorkspaceNav.tsx`; URL domains in all three workspaces, selected-link visibility and navigation focus handling. Implemented. |
| Section primitive | Shared `Section.tsx` provides heading association, description, actions and danger tone. Adopted in account panels; server/routing still use local panel markup. Partial adoption. |
| Drawer / focused detail | Inline selected details exist for devices, rules and configuration versions. Focused-detail option implemented; a generic drawer is unnecessary unless a concrete workflow requires an overlay. |
| Notice / Callout | Feedback uses repeated `form-message` markup with inconsistent alert/status roles. Shared Notice component is absent; the earlier wording implied otherwise. Pending consolidation. |
| Action Menu | Scoped buttons and native disclosures are in use. Generic Action Menu is absent. Record an explicit retain-disclosure decision or implement it for a concrete action group before claiming full foundation completion. |
| Summary / Status | Shared `StatusBadge.tsx` exists, alongside local badges and domain-specific summaries. A unified Summary contract is not yet documented/implemented. Partial. |
| Account domains / identity | Overview, access, routing, protocols, traffic and settings; persistent account header and account-keyed detail subtree. Implemented. |
| Server / routing domains | Six server domains and three profile domains; identity remains visible. Hidden domain panels retain local state. Implemented. |
| Draft preservation | Routing protects dirty settings against refetch and retains mounted forms. Account domain components are conditionally rendered and unmount on tab changes: local settings input can be lost. Follow-up required. |
| Guided next action | Account/server/routing overviews and Dashboard supply contextual links. Implemented baseline; no navigation-triggered mutations introduced. |
| Responsive / accessibility | Shared navigation and focused details exist; current permanent browser checks cover only selected mobile states. Full matrix remains outstanding. |

### Concrete follow-ups, in order

1. Preserve account settings drafts across domain navigation and background data
   refresh without carrying drafts, revealed credentials or confirmations to another
   account. `VpnAccountsPage.tsx` conditionally mounts each panel, while
   `VpnAccountManagementPanel.tsx` stores fields in local state and initializes them
   from query data. Add a real-browser regression for navigation away/back and
   switching account identity. Audit other account forms for the same lifecycle.
2. Extend `frontend/scripts/workspace-integration.mjs` to cover all three workspaces
   at narrow and wide widths in both themes, including the open routing editor.
   Today server tabs are visited at desktop width; mobile routing is checked on
   settings and mobile account on access. Those checks are not a full mobile matrix.
3. Consolidate actionable feedback into a shared Notice with explicit neutral,
   success and error semantics. Document a Summary/Status contract and decide the
   remaining Action Menu scope based on actual consumers. Avoid unused primitives
   created only to satisfy a component-name checklist.
4. Recheck keyboard focus, long names, loading/error/empty states and confirmations
   after these changes; then record owner visual acceptance and remaining deferred
   scope explicitly. Only then decide whether to close RG-80.

Existing CI on PR #469 and its merged main commit passed, including the real
Manager/PostgreSQL browser job. Those results cover the script's existing assertions;
they do not verify the follow-ups above. Runtime health validation after deployment
also passed, but is separate from UI acceptance and real client traffic testing.
