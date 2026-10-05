# Manager session UI repair on US

The original session repair was published from `ebd94d87` in run 37324231784.
The Manager binary was subsequently published from merged main `c4137cc4` in
run 37339500807. The current manual UI workflow is pinned to `6db6e4bcf9f5c547a440d1ea2586a2ebae3665ea` for the transfer-notice and subscription-copy follow-up in PR #525; its two exact CI runs must pass before publication.
The host already has schema159; the old schema158-to159 deployment helper
must not be reused.

Run **RG140 Manager UI update** from `main`. It builds only the pinned
frontend, validates its digest and archive layout, and publishes new assets
before atomically replacing `/var/www/routegate/index.html`. Existing assets
are retained for open tabs; same-name collisions with different content are
refused. Bootstrap files, Manager/Agent binaries, configuration, schema and
account data are not modified. No service is stopped or restarted. The
read-only preflight requires schema159 and no active transfer.

The previous index is backed up under `/root/routegate-backups/ui-session-…`.
Failure of the public index check or protected-file/service comparison restores
that previous index. The resulting UI commit is distinct from the unchanged
Manager binary commit: this is a frontend-only repair, not the full PR rollout.

After workflow success, reload the Manager tab once to load the new UI. Existing
open tabs continue using their old bundle until reloaded. With a valid session,
temporary failure of `/api/admin/me` must preserve sign-in and the current
workspace, showing **Retry session check**. After connectivity returns, retry
must recover without a password. A real HTTP 401 still clears invalid sign-in.

The workflow is manual only; merging this helper performs no server update.
RG-140 broader security/recovery UX acceptance remains separate from this scoped UI publication. All three VPS remain, and US stays Hybrid.
