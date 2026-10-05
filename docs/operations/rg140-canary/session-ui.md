# Manager session UI repair on US

The session fix is tested on candidate `ebd94d874c3d25f49a6e2e9a7d06fd35e6f5b481`
in PR #511. Both exact candidate CI runs are pinned in the manual workflow.
The installed Manager canary already has schema159; the old schema158-to159
deployment helper must not be reused for this repair.

Run **RG140 Manager session UI update** from `main`. It builds only the pinned
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
Full feature merge/rollout approval and remaining client acceptance for RG-140
are separate from this repair.
