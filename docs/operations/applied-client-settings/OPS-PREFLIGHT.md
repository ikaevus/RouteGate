# Applied client settings preflight: provenance

`preflight-schema-155.sql` in this directory is the read-only preflight of the
applied-client-settings update (PR #499), added to `main` ahead of that PR so
the Production-like Ops operation `preflight-applied-client-settings-155` can
check the Manager database before the update is merged.

| | |
|---|---|
| Source | `docs/operations/applied-client-settings/preflight-schema-155.sql` |
| Commit | `b72637cf122d4d3db0c988ebb436e87bbe00d6a9` (branch `claude/applied-client-settings`) |
| Git blob | `422878a4f53327c297b60e9e97d410402ec47f23` |
| SHA-256 | `324095e8671a2cb1bcf2f30e41b9e1658e27718a1817d32df5e1e7bf41182a49` |

The file is a byte-for-byte copy; do not edit it here. The host runner
`scripts/production-like-preflight-applied-client-settings.sh` pins the same
SHA-256 and refuses any other file. A changed preflight needs a new pinned copy,
checksum and review.

How to read P0–P7, what blocks the update, and the post-update checks are
documented with the update itself (README of that directory in PR #499). The
operation, its safeguards and its output are described in
`docs/operations/production-like-ops-bridge.md`.
