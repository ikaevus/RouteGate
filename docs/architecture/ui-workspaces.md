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

Use Notice for actionable feedback. A failed operation should leave the form and
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

The account workspace is the reference for gradual adoption. Existing server and
routing screens need separate review; this slice does not claim full UI migration.

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
