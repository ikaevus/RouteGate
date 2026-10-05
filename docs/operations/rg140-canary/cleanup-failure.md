# One-job cleanup failure acceptance

This manual main-only workflow does not deploy a release or mutate database
state. Use only after fi-test has a verified FI → US cutover and before source
cleanup. Keep the retained FI access available. Run the workflow, wait for its
`armed` message, then use **Restore source and clean target**, acknowledging that
clients must refresh back to FI. Never choose Remove source access for this test.

The helper watches that exact operation for a pending US target-cleanup job.
It creates an empty directory at **only that config version's `.json.tmp` staging
path**. The genuine Agent write fails before config promotion, validation or VPN
restart and the Agent reports failure normally. There are no fabricated reports,
SQL writes, proof exceptions or shared-service stops. Protected config hashes and
service identities must remain unchanged for the failure to count.

The empty fault is removed in `finally`; an independent systemd timer also removes
it after two minutes if the helper or SSH disappears. The helper never overwrites
an existing candidate, follows a staging symlink, or recursively removes files.
It refuses other pending jobs and non-default staging layouts. If the Agent wins
the race, the helper refuses to claim a successful failure test.

After `temporary_fault=removed`, Verify completion must refuse the failed job.
Use **Retry failed apply**, wait for the genuine Agent result, then **Verify
completion**. Expected result is **Source restored**, exact cleanup membership
absence, FI subscriptions still usable, unrelated US accounts unchanged.

Known limitation: the pending-job observation and filesystem fault are not one
atomic operation. A missed race is safe but inconclusive; do not edit job state
or claim acceptance. If identity changes or the directory becomes nonempty,
the helper refuses destructive recovery and reports the need for manual review.
