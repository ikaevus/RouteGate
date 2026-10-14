#!/usr/bin/env bash
# Static fail-closed wiring checks. Never contacts US or dispatches workflows.
set -Eeuo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
workflow="$repo/.github/workflows/rg140-vless-preview-manager-release.yml"
runner="$repo/scripts/production-like-rg140-manager-main.sh"
python3 - "$workflow" "$runner" <<'PY'
import pathlib
import sys

workflow = pathlib.Path(sys.argv[1]).read_text()
runner = pathlib.Path(sys.argv[2]).read_text()
candidate = "ff377ec1eea5889f37e409d027fb40a2b6562951"
previous = "3e8d6c72984fa848551b59004029bc6cfcbd0b30"
legacy = "567a1e154254eebb7f8ee95e46a97c5d0b3c8e4a"
oldest = "c4137cc43a2385ac38fe71e059f33405535b6618"
assert workflow.startswith("name: RG140 VLESS preview Manager release")
assert "on:\n  workflow_dispatch:\n" in workflow
assert "\n  push:" not in workflow and "\n  schedule:" not in workflow
assert "if: github.ref == 'refs/heads/main'" in workflow
assert "environment: production-like" in workflow
assert "group: routegate-production-like-deploy" in workflow
assert "cancel-in-progress: false" in workflow
assert "CANDIDATE: " + candidate in workflow
assert "ref: " + candidate in workflow
assert "COMMIT: " + candidate in workflow
assert "AGENT_BUNDLE_BASE_URL: https://us.routegate.org/bootstrap/" + candidate in workflow
assert "for spec in '38606266284|RouteGate CI' '38606266865|Workspace integration'; do" in workflow
assert "37823646085|RouteGate CI" not in workflow
assert "37823646080|Workspace integration" not in workflow
assert "GH_TOKEN: ${{ github.token }}" in workflow
assert "RELEASE_APPROVAL: ${{ inputs.approval }}" in workflow
assert "[[ \"$RELEASE_APPROVAL\" == 'deploy-US-manager-only-ff377ec1' ]]" in workflow
assert "38606266284|RouteGate CI" in workflow
assert "38606266865|Workspace integration" in workflow
assert "completed|success|push|main|$expected_name" in workflow
assert "sudo bash scripts/test-rg140-manager-main.sh \"$CANDIDATE\"" in workflow
assert "scripts/build-release-bundle.sh" in workflow
assert "sha256sum -c SHA256SUMS" in workflow
assert "sudo -n flock -w 600 /run/lock/routegate-production-like.lock" in workflow
assert "production-like-rg140-manager-main.sh" in workflow
assert "StrictHostKeyChecking=yes" in workflow
assert "ROUTEGATE_KNOWN_HOSTS" in workflow
assert "trap cleanup EXIT" in workflow
assert workflow.index("Verify approval and successful candidate checks") < workflow.index("Configure SSH")
assert workflow.index("Test backup and recovery runner before SSH") < workflow.index("Configure SSH")
assert oldest in runner and legacy in runner and previous in runner and candidate in runner
assert "case \"$EXPECTED_COMMIT\" in" in runner
assert "trap on_signal HUP INT TERM" in runner
assert "SCHEMA_TARGET=000159_staged_account_transfers" in runner
assert "rg_update_create_management_backup" in runner
assert "rg_update_restore_management_backup" in runner
assert "IDENTITY_BEFORE=$(continuity_identity)" in runner
assert "fingerprint >\"$WORK_DIR/fingerprint.before\"" in runner
assert "RESULT=updated commit=" in runner
print("pinned manual Manager-only release wiring: PASS")
PY
