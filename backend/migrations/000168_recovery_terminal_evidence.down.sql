DO $$ BEGIN
 LOCK TABLE runtime_recovery_terminal_evidence IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM runtime_recovery_terminal_evidence) THEN
 RAISE EXCEPTION 'recovery_terminal_evidence_retained'; END IF;
 DROP TABLE runtime_recovery_terminal_evidence;
 DROP FUNCTION routegate_recovery_terminal_evidence_guard();
 DROP FUNCTION routegate_recovery_terminal_wire(json);
 DROP FUNCTION routegate_recovery_terminal_instant(text);
END; $$;
