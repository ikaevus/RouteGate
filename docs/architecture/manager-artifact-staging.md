# Manager-Owned Artifact Staging Boundary

RG-96C3b connects successful fixed-source release discovery to the existing non-mutating RouteGate release verifier. It deliberately stops before privileged host mutation.

## Purpose

A Manager with an authenticated administrator request may create a durable `stage` update job from one previously completed C2 discovery job. The stage job downloads the exact official release assets selected by that discovery result into Manager-owned private state, invokes the C3a non-mutating verifier, persists only trusted descriptor metadata, and then stops.

A successful stage job means: the candidate bytes were downloaded from the fixed RouteGate GitHub Release namespace and the local snapshot passed the RouteGate release trust gate. It does **not** authorize installation, database migration, service restart, rollback, or any other privileged host operation.

## API boundary

The stage endpoint accepts only:

```json
{
  "discoveryJobId": "<UUID>"
}
```

The referenced job must already be a successful canonical `discovery` job whose result is stageable. The caller cannot supply a release URL, repository, version, asset name, filesystem path, verifier path, signer identity, trust root, command, architecture, or operating system.

Candidate version, runtime platform, and the required asset names/sizes are reconstructed from the durable C2 discovery result and revalidated before a stage job is inserted.

## Fixed download contract

For a stageable candidate, Manager downloads exactly the C2-required asset set from the fixed namespace:

`https://github.com/ikaevus/RouteGate/releases/download/<version>/<asset>`

The required set is:

- `release-manifest.json`;
- `release-manifest.attestation.json`;
- `SHA256SUMS`;
- `release-bundles.attestation.json`;
- the matching `routegate-<version>-linux-<arch>.tar.gz` bundle.

Redirects must remain inside the allowed GitHub HTTPS release-asset boundary. Response bodies and verifier output are bounded. The discovered asset sizes are enforced while downloading; a size mismatch is a staging failure.

Remote response bodies, raw verifier stderr, arbitrary URLs, and filesystem paths are not persisted in the update-job result.

## Private staging state

Production staging lives under the fixed Manager state directory:

`/var/lib/routegate-manager/update-staging/<stage-job-id>`

Downloads first enter `<stage-job-id>.partial`. The staging root must be a real directory owned by the Manager process user and must not be group/world accessible. A failed operation removes both partial and finalized state for that job.

Only after every required asset has been downloaded and the C3a verifier has returned a valid trusted descriptor is the private directory atomically renamed from `.partial` to its final job ID.

The durable `StageResult` stores verified release metadata, not the staging directory path. A later privileged slice must derive any local path from the trusted job ID and fixed RouteGate state layout rather than accepting a path through HTTP or durable request data.

## Verifier boundary

Manager invokes only the fixed non-root verifier:

`/usr/local/lib/routegate/update/routegate-update-verified.sh verify`

Before execution, Manager validates the trusted updater path hierarchy so the executable cannot be replaced through a symlink or an untrusted writable path. The C3a verifier independently validates its pinned attestation-verification runtime and enforces the fixed RouteGate provenance policy.

Successful verifier stdout must decode as the bounded trusted descriptor defined by C3a. Manager then cross-checks the descriptor against the candidate selected by C2, including version, OS, architecture, artifact name/size, commit/digest shape, and expected migration identifier.

The future privileged apply path must verify the staged release again immediately before mutation. C3b verification is therefore evidence for staging and operator workflow, not a time-of-check authorization token for host mutation.

## Durable lifecycle and restart recovery

C3b extends the existing `update_jobs` history with `operation=stage` and `stage=stage` using the same durable lifecycle:

`pending -> running -> succeeded|failed`

The HTTP request context is detached from the bounded stage lifecycle after authentication and input validation so an administrator closing the browser does not strand an already-created job in an ambiguous client-dependent state.

If Manager restarts with a `pending` or `running` stage job, startup recovery terminalizes it as failed with the safe `stage_interrupted` code and removes both partial and finalized staging state for that job before serving the normal API. Cleanup failure is a startup recovery error rather than silently retaining orphaned candidate bytes.

Lifecycle transitions use the existing audit subsystem. Persisted failure codes are bounded RouteGate-defined codes; raw network or verifier diagnostics are not written into durable job state or returned to the caller.

## Explicit non-goals

RG-96C3b does not add or invoke:

- `sudo` or arbitrary command execution;
- `/usr/local/sbin/routegate-update apply`;
- the privileged B2 host transaction;
- systemd, package, binary, UI, migration, or VPN runtime mutation;
- backup or rollback execution;
- automatic or background update checks;
- release-channel configuration;
- Admin UI controls;
- multi-node Agent rollout.

The security boundary after C3b is therefore:

```text
C2 successful discovery
        |
        v
C3b Manager-owned private staging
        |
        v
C3a non-mutating release verification
        |
        v
verified staged candidate
        |
        X  STOP: no host mutation in C3b
```

Privileged dispatch/apply must be implemented and reviewed as a separate security slice.
