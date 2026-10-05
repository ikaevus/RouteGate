-- Fixed read-only diagnostics. Never return credentials, config bodies or links.
BEGIN READ ONLY;
SELECT t.id,t.state,t.target_job_id,t.target_version_id,
 t.cutover_at IS NOT NULL AS cutover_done,
 t.cleanup_acknowledged_at IS NOT NULL AS cleanup_acknowledged,
 a.server_id=t.source_server_id AS still_on_source,
 CASE WHEN t.last_error IN ('runtime_evidence_missing','runtime_listener_not_verified')
 THEN t.last_error ELSE CASE WHEN t.last_error='' THEN '' ELSE 'other_error' END END AS error_code
FROM vpn_account_transfers t JOIN vpn_accounts a ON a.id=t.vpn_account_id
WHERE t.vpn_account_id='83ef4f8b-78a6-474b-8dd7-c32c1b71fe66' AND t.completed_at IS NULL;
SELECT j.id,j.status,j.agent_id IS NOT NULL AS agent_assigned,
 j.completed_at IS NOT NULL AS completed,
 cv.id=s.active_config_version_id AS current_version,
 cv.client_settings->>'vlessPort' AS expected_port,
 CASE WHEN j.result_payload->>'stage' IN ('succeeded','failed','skipped_service_control_disabled')
 THEN j.result_payload->>'stage' ELSE 'missing_or_unknown' END AS report_stage,
 jsonb_typeof(j.result_payload->'components') AS components_type,
 CASE WHEN jsonb_typeof(j.result_payload->'components')='array'
 THEN jsonb_array_length(j.result_payload->'components') END AS component_count,
 cv.client_settings->'accounts' ? t.vpn_account_id::text AS test_account_in_version
FROM vpn_account_transfers t JOIN config_apply_jobs j ON j.id=t.target_job_id
JOIN config_versions cv ON cv.id=j.config_version_id JOIN servers s ON s.id=j.server_id
WHERE t.vpn_account_id='83ef4f8b-78a6-474b-8dd7-c32c1b71fe66' AND t.completed_at IS NULL;
SELECT CASE WHEN c->>'core' IN ('sing-box','wireguard','hysteria','mtg')
 THEN c->>'core' ELSE 'missing_or_unknown' END AS core,
 CASE WHEN c->>'protocol' IN ('vless','wireguard','hysteria2','shadowsocks','mtproto')
 THEN c->>'protocol' ELSE 'missing_or_unknown' END AS protocol,
 CASE WHEN c->>'status' IN ('succeeded','failed','skipped_service_control_disabled')
 THEN c->>'status' ELSE 'missing_or_unknown' END AS status,
 CASE WHEN c->>'listenerPort' ~ '^[0-9]{1,5}$' THEN c->>'listenerPort' END AS listener_port,
 c ? 'listenerAddress' AS has_listener_address,
 COALESCE(c->>'healthCommand','')<>'' AS has_health_check,
 COALESCE(c->>'restartCommand','')<>'' AS has_restart
FROM vpn_account_transfers t JOIN config_apply_jobs j ON j.id=t.target_job_id
CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(j.result_payload->'components')='array'
 THEN j.result_payload->'components' ELSE '[]'::jsonb END) c
WHERE t.vpn_account_id='83ef4f8b-78a6-474b-8dd7-c32c1b71fe66' AND t.completed_at IS NULL;
ROLLBACK;
