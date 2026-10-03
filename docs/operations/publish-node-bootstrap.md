# Publish packages for new VPN nodes

After a Manager-only update, its Connect server command refers to packages at
`https://us.routegate.org/bootstrap/<manager-commit>/`. Publish those packages
with the `publish-bootstrap` operation instead of performing a full deploy.

The operation requires a full lowercase commit SHA contained in `main`, with a
successful RouteGate CI push run on `main`. Select **Production-like Ops**,
branch **main**, operation **publish-bootstrap**, and that commit. Alternatively,
while Ops Bridge issue #268 is closed, set its body to exactly:

```text
operation=publish-bootstrap commit=<40-character manager commit>
```

Then reopen the issue. Use the commit currently installed on Manager, not
necessarily the latest main commit: adding this operation does not require
updating Manager again. Read the installed identity from its update run.

The workflow builds amd64 and arm64 packages from that exact commit. The
publisher, taken from trusted main, checks the checksum file identity, both
archive hashes, safe archive paths and each manifest's version/commit/OS/arch.
It never extracts or executes the packages on the Manager host.

Under the existing production-like lock, publication writes only a new
`/var/www/routegate/bootstrap/<commit>/` directory. Files are readable by nginx
(directories 0755, files 0644). The directory becomes visible by atomic rename;
other commits are kept. Existing files for the same commit are never replaced:
an identical set is verified again; a different set causes refusal. Builds may
contain timestamps, so rebuilding the same commit is not guaranteed to produce
identical bytes. Do not remove or overwrite an existing publication to force a
repeat to pass; investigate its provenance instead.

Verification downloads all three public files over HTTPS and compares their
actual hashes, rejecting SPA fallback pages even if HTTP status is 200. A failed
probe removes only the directory created by that run. The established nginx
bootstrap route is required; the operation does not edit or reload nginx.

Success is `RESULT=published` or `RESULT=already-published` in the Actions log
and the Ops Bridge comment. Nonzero exit means publication did not complete.
No database access, Manager update, service restart, account change, node
connection, render/apply task, or installation on an existing Agent occurs.
FI and RU are not contacted. Installing a fresh test node is a separate step.

Local verification:

```bash
python3 -m unittest scripts/test_publish_bootstrap.py
bash scripts/test-production-like-preflight.sh
bash scripts/test-production-like-update-manager.sh
```

Tests use temporary files and mocked downloads/SSH, never live infrastructure.
