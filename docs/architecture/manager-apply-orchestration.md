# Manager Apply Orchestration Boundary

RG-96C3d connects a successfully verified C3b stage job to the fixed C3c local privileged dispatch boundary. It adds Manager-owned durable orchestration only; it does not broaden the privileged command language and does not add UI, automatic updates, release channels, or multi-node rollout.

## Purpose

An authenticated administrator may create one durable `apply` update job from one previously completed successful `stage` job. Manager revalidates that the referenced stage result is canonical and local, then sends only that stage job UUID plus a newline to the fixed RouteGate Unix socket:

`/run/routegate/update-dispatch.sock`

No filesystem path, release URL, version, asset name, role, command, trust root, signer, environment assignment, shell syntax, or arbitrary systemd input is accepted through the API or sent across the privilege boundary.

The root-side C3c dispatcher remains authoritative for reconstructing `/var/lib/routegate-manager/update-staging/<uuid>`, validating exact staged contents, re-running the verified release gate, resolving the trusted local node role, and entering the existing transactional host apply/rollback path.

## API boundary

The apply endpoint accepts only:

```json
{
  "stageJobId": "<UUID>"
}
```

The referenced job must be a successful canonical `operation=stage` job with a valid bounded C3b result. Manager must not accept a caller-provided path, repository, version, target role, updater executable, socket path, command, URL, or artifact selector.

A new durable update job is inserted with `operation=apply` and `stage=apply`. The request context is detached after authorization and durable job creation so a browser disconnect does not cancel an already-started privileged transaction.

## Local dispatch client

Manager connects only to the fixed local Unix socket. The request body is exactly the canonical lowercase UUIDv4 stage job ID followed by one newline. The response is bounded to the C3c RouteGate-defined `OK` or `ERR` result.

Manager never reads raw privileged verifier or transaction output through this channel. Detailed host-update diagnostics remain root-side logs; persisted Manager failure data uses bounded RouteGate-defined error codes.

A connection/write failure before the complete canonical UUID request has crossed the socket is a definite apply-job failure with no automatic retry. Once the complete UUID plus newline has been sent, only an exact `ERR` is a definite failure and an exact `OK` is a definite success. Disconnect, timeout, short/invalid response, or other loss of the bounded result after complete request transmission is `apply_outcome_unknown`; it is never automatically replayed.

## Staged-candidate lifetime

The source C3b staged candidate must remain present while privileged apply may still be using it. Manager therefore creates a private persistent apply pin under the fixed staging root before crossing the C3c boundary. Normal retained-stage eviction checks this pin and fails closed rather than removing a pinned candidate.

On a definite `ERR` or a confirmed `OK`, the pin is released after Manager finishes the corresponding durable lifecycle handling. If the Manager crashes or the dispatch result becomes ambiguous after the complete request was sent, the pin remains. This intentionally favors retained disk usage over deleting files that a still-running root transaction may be reading.

An unresolved `apply_outcome_unknown` may therefore consume one retained-stage slot until a future explicit reconciliation/cleanup workflow determines that releasing the pin is safe. Automatic stale-pin cleanup is not part of C3d because time alone cannot prove that an ambiguous privileged transaction has stopped.

## Restart and unknown-outcome recovery

The privilege boundary deliberately allows the root transaction to continue even if the Manager connection disappears. Therefore a Manager process crash or restart can leave a previously `running` apply job whose privileged transaction outcome is no longer safely knowable from Manager state alone.

RG-96C3d must fail closed in this case:

- startup recovery terminalizes interrupted `pending`/`running` apply jobs with a bounded `apply_outcome_unknown` failure code;
- it must not automatically replay the stage UUID;
- it must not infer success solely from the old C3b stage result;
- it must not delete the staged candidate or its persistent apply pin as part of unknown-outcome recovery;
- a later operator workflow may run fresh preflight/version checks before deciding whether a new apply attempt or pin release is safe.

This avoids duplicate host mutation after an ambiguous Manager restart. Durable privileged receipts or resumable apply reconciliation, if desired later, require a separate reviewed contract rather than being inferred in C3d.

## Concurrency and lifecycle

Manager must permit at most one non-terminal local `apply` job at a time. Manager also serializes ordinary C3b retained-stage admission while an in-process apply is active, while the persistent pin preserves the critical candidate-lifetime invariant across Manager failure. The existing root transaction lock remains the final privileged serialization boundary and is not replaced by Manager-side coordination.

The durable lifecycle remains:

`pending -> running -> succeeded|failed`

Audit events are written through the existing update-job audit mechanism. Successful apply results persist bounded metadata sufficient for operator history, including the source stage job ID and trusted release descriptor already present in the stage result; they do not persist privileged filesystem paths or raw root output.

## Explicit non-goals

RG-96C3d does not add:

- Admin UI controls or one-click update UX;
- background polling or automatic apply;
- release-channel configuration;
- scheduled maintenance windows;
- multi-node or Agent rollout;
- caller-selected node roles;
- caller-selected privileged commands or paths;
- arbitrary root/systemd access;
- durable privileged receipt/reconciliation protocol;
- automatic stale-pin reconciliation or staged-artifact garbage collection policy.

Those remain separate slices after the local Manager-to-host apply path is complete.

## Security boundary

```text
successful C3b stage job UUID
        |
        v
Manager durable apply job
        |
        +--> private persistent stage pin
        |
        | exact UUID + newline only
        v
fixed local Unix socket
        |
        v
C3c root dispatcher
        |
        v
re-verify staged release
        |
        v
role-aware transactional apply + rollback
```

C3d therefore adds orchestration, not authority: all privileged authority remains inside the already reviewed C3c/B2 boundary.
