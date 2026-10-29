# RG-140 administrative lock coverage

Status: partial serialization, not conversion/recovery admission. Issue #545 is open.

The host updater, `routegate-recovery` and `install.sh` use the same default lock identity,
`/run/lock/routegate-update.lock`. Recovery acquires it before the legacy
`/run/lock/routegate-recovery.lock`, before log initialization and before operation
state reads or service/config changes. Both locks are nonblocking and remain held
through operation failure handling. Keeping the second lock excludes an older
recovery tool; an old recovery tool still does not exclude the updater by itself.
All three callers honor `RG_UPDATE_LOCK_FILE`; different overrides defeat this
coordination and are not a supported shared-host serialization configuration.

The installer acquires the shared lock before its legacy
`/run/lock/routegate-installer.lock`, before log initialization and input/preflight
steps. Its former truncating legacy-lock open is now append-only. Old installers
still serialize with the updated installer via the legacy lock; they do not gain
coordination with updater/recovery. Lock contention prints to stderr without
creating or modifying the installation log.

The real-flock fixture executes recovery's actual main with only its root check
and operation body substituted. It tests updater/recovery contention in both
directions, old-lock/recovery contention in both directions, recovery/recovery,
unchanged lock bytes/inodes, refusal before log creation, and acquisition after
holder death. The updater function is extracted from its actual source. These
checks do not prove child-descriptor lifetime or power-loss behavior.

The same fixture executes the installer's real main through its first input step,
substituting the root check, privileged log initialization and input handler (which
stops before host work). It tests installer/updater, installer/recovery and
installer/legacy-installer contention in both directions, plus installer/installer.
Each loser is tested with both absent and existing logs; neither is changed.
Default shared-lock identity is checked for all three entry points.

## Remaining entry points and boundaries

- Production-like deploy, Manager update, repair and operational workflows use
  `/run/lock/routegate-production-like.lock` in their SSH wrappers. Their script
  entry points are not all self-locking. They remain separate from the updater
  lock; simply changing the wrapper path and adding the same lock inside would
  cause self-contention through independently opened descriptors.
- Agent config application and direct systemd/binary invocation do not acquire
  this administrative lock. The lock does not prevent an Agent apply racing a
  recovery config rollback, or establish a protected startup gate.
- The unwired pin writer still requires a future caller to hold the host lock.
  No conversion completion or pending-marker removal is enabled here.
- Path ancestry, symlink/replacement attacks, inconsistent overrides, inherited
  descriptors and a versioned startup/rollback policy remain unresolved. This
  change does not strengthen the lock path trust model.

Before conversion activation, establish one lock and descriptor ownership
contract across all participating entry points, validate authority under that
lock, and test nested invocation and process death with inherited children.
Credential preservation, retired UUID rejection, full linked E2E and the final
security/race/failure audit remain separate requirements.
