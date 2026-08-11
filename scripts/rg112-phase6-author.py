from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one replacement target, found {count}")
    p.write_text(text.replace(old, new, 1))


replace_once(
    "install.sh",
    'ROUTEGATE_HTTP_ADDR="127.0.0.1:8080"\nROUTEGATE_DATABASE_URL=',
    'ROUTEGATE_HTTP_ADDR="127.0.0.1:8080"\nROUTEGATE_PUBLIC_URL="https://${ROUTEGATE_DOMAIN}"\nROUTEGATE_DATABASE_URL=',
)

installer_test = r'''test_manager_environment_public_url() {
  local original_manager_env="$ROUTEGATE_MANAGER_ENV"
  ROUTEGATE_MANAGER_ENV="$TEST_TMP/manager.env"
  ROUTEGATE_DOMAIN="us.routegate.org"
  ROUTEGATE_ADMIN_EMAIL="admin@example.org"

  write_manager_environment "fixture-db-password" "fixture-admin-password"

  assert_true \
    "manager environment includes the canonical public URL" \
    grep -Fxq 'ROUTEGATE_PUBLIC_URL="https://us.routegate.org"' "$ROUTEGATE_MANAGER_ENV"
  assert_equal \
    "manager environment remains private" \
    "600" \
    "$(stat -c '%a' "$ROUTEGATE_MANAGER_ENV")"

  ROUTEGATE_MANAGER_ENV="$original_manager_env"
}

'''
replace_once(
    "scripts/test-clean-vps-installer.sh",
    "test_agent_credentials_detection() {\n",
    installer_test + "test_agent_credentials_detection() {\n",
)
replace_once(
    "scripts/test-clean-vps-installer.sh",
    "test_setup_url_contract\ntest_confirmation_prompt",
    "test_setup_url_contract\ntest_manager_environment_public_url\ntest_confirmation_prompt",
)

workflow = ".github/workflows/production-like-deploy.yml"
replace_once(
    workflow,
    '          SESSION_ID=""\n          MUTATED=0',
    '          SESSION_ID=""\n          DELIVERY_FIXTURE_ID=""\n          MUTATED=0',
)
replace_once(
    workflow,
    '''            if [[ -n "${SESSION_ID:-}" && -n "${DB_URL:-}" ]]; then
              psql "$DB_URL" -v ON_ERROR_STOP=1 -qAtc \\
                "DELETE FROM auth_sessions WHERE id='${SESSION_ID}'::uuid" >/dev/null 2>&1 || true
            fi
            rm -rf "$WORK_DIR"''',
    '''            if [[ -n "${SESSION_ID:-}" && -n "${DB_URL:-}" ]]; then
              psql "$DB_URL" -v ON_ERROR_STOP=1 -qAtc \\
                "DELETE FROM auth_sessions WHERE id='${SESSION_ID}'::uuid" >/dev/null 2>&1 || true
            fi
            if [[ -n "${DELIVERY_FIXTURE_ID:-}" && -n "${DB_URL:-}" ]]; then
              psql "$DB_URL" -v ON_ERROR_STOP=1 -qAtc \\
                "DELETE FROM deliveries WHERE id='${DELIVERY_FIXTURE_ID}'::uuid" >/dev/null 2>&1 || true
            fi
            rm -rf "$WORK_DIR"''',
)
replace_once(
    workflow,
    '''              install -m 0644 "$BACKUP_DIR/routegate-manager.service" /etc/systemd/system/routegate-manager.service
              install -m 0644 "$BACKUP_DIR/routegate-agent.service" /etc/systemd/system/routegate-agent.service

              if [[ -s "$BACKUP_DIR/sing-box-config.json" ]]; then''',
    '''              install -m 0644 "$BACKUP_DIR/routegate-manager.service" /etc/systemd/system/routegate-manager.service
              install -m 0644 "$BACKUP_DIR/routegate-agent.service" /etc/systemd/system/routegate-agent.service
              install -m 0600 "$BACKUP_DIR/manager.env" /etc/routegate/manager.env

              if [[ -s "$BACKUP_DIR/sing-box-config.json" ]]; then''',
)
replace_once(
    workflow,
    '''          cp -a /etc/systemd/system/routegate-manager.service "$BACKUP_DIR/routegate-manager.service"
          cp -a /etc/systemd/system/routegate-agent.service "$BACKUP_DIR/routegate-agent.service"
          tar -czf "$BACKUP_DIR/manager-migrations.tar.gz"''',
    '''          cp -a /etc/systemd/system/routegate-manager.service "$BACKUP_DIR/routegate-manager.service"
          cp -a /etc/systemd/system/routegate-agent.service "$BACKUP_DIR/routegate-agent.service"
          cp -a /etc/routegate/manager.env "$BACKUP_DIR/manager.env"
          tar -czf "$BACKUP_DIR/manager-migrations.tar.gz"''',
)
replace_once(
    workflow,
    '''          DB_MAY_BE_MUTATED=1
          systemctl start routegate-manager''',
    '''          if grep -q '^ROUTEGATE_PUBLIC_URL=' /etc/routegate/manager.env; then
            sed -i 's#^ROUTEGATE_PUBLIC_URL=.*#ROUTEGATE_PUBLIC_URL="https://us.routegate.org"#' /etc/routegate/manager.env
          else
            printf '\nROUTEGATE_PUBLIC_URL="https://us.routegate.org"\n' >> /etc/routegate/manager.env
          fi
          chmod 0600 /etc/routegate/manager.env
          grep -Fxq 'ROUTEGATE_PUBLIC_URL="https://us.routegate.org"' /etc/routegate/manager.env

          DB_MAY_BE_MUTATED=1
          systemctl start routegate-manager''',
)
replace_once(
    workflow,
    '''            000111_traffic_usage_daily_rollup \\
            000112_agent_runtime_metrics; do''',
    '''            000111_traffic_usage_daily_rollup \\
            000112_agent_runtime_metrics \\
            000113_delivery_foundation; do''',
)
replace_once(
    workflow,
    "          assert version['database']['expectedSchemaVersion'] == 112, version['database']\n          assert version['database']['appliedSchemaVersion'] == '000112_agent_runtime_metrics', version['database']",
    "          assert version['database']['expectedSchemaVersion'] == 113, version['database']\n          assert version['database']['appliedSchemaVersion'] == '000113_delivery_foundation', version['database']",
)

delivery_validation = r'''          FORBIDDEN_DELIVERY_COLUMNS="$(psql "$DB_URL" -qAtc "
            SELECT COUNT(*)
            FROM information_schema.columns
            WHERE table_schema='public'
              AND table_name='deliveries'
              AND column_name IN (
                'body', 'message_body', 'rendered_body', 'rendered_html', 'vless_uri',
                'connect_url', 'qr_payload', 'qr_image', 'provider_response',
                'raw_provider_response', 'smtp_password', 'bot_token', 'access_token'
              )
          ")"
          [[ "$FORBIDDEN_DELIVERY_COLUMNS" == "0" ]]

          curl -fsS -H "Authorization: Bearer $TOKEN" "$API/api/v1/delivery/providers" > "$WORK_DIR/providers.json"
          python3 - "$WORK_DIR/providers.json" <<'PY_DELIVERY_PROVIDERS'
          import json
          import sys

          with open(sys.argv[1], encoding='utf-8') as f:
              payload = json.load(f)
          items = payload.get('items')
          assert isinstance(items, list), payload
          channels = {item.get('channel') for item in items}
          assert {'email', 'telegram', 'whatsapp'} <= channels, items

          forbidden_tokens = ('password', 'secret', 'token', 'credential', 'accessurl', 'vless', 'rawresponse')
          def walk(value):
              if isinstance(value, dict):
                  for key, child in value.items():
                      normalized = ''.join(ch for ch in key.lower() if ch.isalnum())
                      assert not any(token in normalized for token in forbidden_tokens), key
                      walk(child)
              elif isinstance(value, list):
                  for child in value:
                      walk(child)
          walk(payload)
          print(f"delivery provider readiness validated: channels={sorted(channels)}")
          PY_DELIVERY_PROVIDERS

          DELIVERY_FIXTURE_ID="$(psql "$DB_URL" -qAtc "
            INSERT INTO deliveries (
              channel, provider, recipient, template_key, locale,
              status, attempt_count, max_attempts, attempt_started_at
            ) VALUES (
              'email', 'smtp', 'rg112-restart-fixture@example.invalid',
              'vpn_access', 'en', 'sending', 1, 5, now()
            )
            RETURNING id::text
          ")"
          [[ -n "$DELIVERY_FIXTURE_ID" ]]

          systemctl restart routegate-manager
          manager_ready=0
          for _ in $(seq 1 30); do
            if curl -fsS "$API/api/admin/health" >/dev/null; then
              manager_ready=1
              break
            fi
            sleep 1
          done
          [[ "$manager_ready" == "1" ]]

          recovery_state=""
          for _ in $(seq 1 10); do
            recovery_state="$(psql "$DB_URL" -qAtc "
              SELECT status || '|' || COALESCE(last_error_class, '') || '|' || COALESCE(last_error_code, '')
              FROM deliveries
              WHERE id='${DELIVERY_FIXTURE_ID}'::uuid
            ")"
            [[ "$recovery_state" == "uncertain|uncertain|manager_restart" ]] && break
            sleep 1
          done
          [[ "$recovery_state" == "uncertain|uncertain|manager_restart" ]]

          psql "$DB_URL" -v ON_ERROR_STOP=1 -qAtc \\
            "DELETE FROM deliveries WHERE id='${DELIVERY_FIXTURE_ID}'::uuid" >/dev/null
          DELIVERY_FIXTURE_ID=""
'''
replace_once(
    workflow,
    '''          API="http://127.0.0.1:8080"
          curl -fsS -H "Authorization: Bearer $TOKEN" "$API/api/v1/system/version" > "$WORK_DIR/version.json"''',
    '''          API="http://127.0.0.1:8080"
''' + delivery_validation + '''
          curl -fsS -H "Authorization: Bearer $TOKEN" "$API/api/v1/system/version" > "$WORK_DIR/version.json"''',
)
replace_once(workflow, 'RG-111 E2E failed after deployment started; restoring production-like baseline.', 'RouteGate E2E failed after deployment started; restoring production-like baseline.')
replace_once(workflow, 'BACKUP_DIR="/root/routegate-backups/rg111-${EXPECTED_COMMIT}-$(date -u +%Y%m%dT%H%M%SZ)"', 'BACKUP_DIR="/root/routegate-backups/rg112-${EXPECTED_COMMIT}-$(date -u +%Y%m%dT%H%M%SZ)"')
replace_once(workflow, 'echo "RG-111 production-like E2E PASSED"', 'echo "RG-112 production-like E2E PASSED"')
