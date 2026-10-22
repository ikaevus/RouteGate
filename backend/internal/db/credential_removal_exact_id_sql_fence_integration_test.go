package db

import (
 "errors"
 "testing"

 "github.com/jackc/pgx/v5/pgconn"
)

// A real human dispatch approval and exact proposed version/job IDs are NOT
// permission to insert ordinary config versions or jobs. The reserved node
// stays protected by the original P0141 trigger until a separately reviewed,
// narrowly scoped atomic dispatch path has been implemented.
func TestCredentialRemovalApprovedIDsStillBlockGenericApply(t *testing.T) {
 f:=newDispatchGateFixture(t)
 if err:=f.svc.CheckPreparedRemovalDispatchAdmission(f.ctx,f.operation,f.nonce,true);err!=nil{
  t.Fatal("test must start with a genuinely valid human approval",err)
 }
 var beforeVersions,beforeJobs,beforeNext int
 var activeVersion string
 if err:=f.pool.QueryRow(f.ctx,`SELECT
  (SELECT count(*) FROM config_versions),
  (SELECT count(*) FROM config_apply_jobs),
  s.next_config_version,s.active_config_version_id::text
 FROM servers s JOIN vpn_credential_revocations r ON r.server_id=s.id
 WHERE r.id=$1::uuid`,f.operation).Scan(
 &beforeVersions,&beforeJobs,&beforeNext,&activeVersion);err!=nil{t.Fatal(err)}
 // Compare complete durable rows, not just counts: a rejected statement must
 // not change authorization, bindings, events, account data or executable rows.
 snapshot := func() string {
  t.Helper()
  var state string
  if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
   'operation',to_jsonb(r),
   'binding',(SELECT to_jsonb(b) FROM vpn_credential_revocation_dispatch_bindings b WHERE b.operation_id=r.id),
   'approval',(SELECT to_jsonb(a) FROM vpn_credential_removal_dispatch_authorizations a WHERE a.operation_id=r.id),
   'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY sequence) FROM vpn_credential_revocation_events e WHERE e.operation_id=r.id),
   'server',(SELECT to_jsonb(s) FROM servers s WHERE s.id=r.server_id),
   'account',(SELECT to_jsonb(a) FROM vpn_accounts a WHERE a.id=r.vpn_account_id),
   'versions',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM config_versions v),
   'jobs',(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM config_apply_jobs j)
  )::text FROM vpn_credential_revocations r WHERE r.id=$1::uuid`,f.operation).Scan(&state); err != nil { t.Fatal(err) }
  return state
 }
 beforeState := snapshot()
 cases:=[]struct{name,query string}{
  {"exact_prepared_candidate_version",`
INSERT INTO config_versions(id,server_id,version,config_hash,status,rendered_config)
SELECT b.candidate_version_id,r.server_id,s.next_config_version,
 r.candidate_hash,'validated',r.candidate_config
FROM vpn_credential_revocation_dispatch_bindings b
JOIN vpn_credential_revocations r ON r.id=b.operation_id
JOIN servers s ON s.id=r.server_id WHERE b.operation_id=$1::uuid`},
  {"exact_prepared_apply_job",`
INSERT INTO config_apply_jobs(id,server_id,agent_id,config_version_id,action,status,request_payload)
SELECT b.apply_job_id,r.server_id,r.agent_id,r.baseline_version_id,
 'apply','pending','{"kind":"vless_credential_removal_v1"}'::jsonb
FROM vpn_credential_revocation_dispatch_bindings b
JOIN vpn_credential_revocations r ON r.id=b.operation_id
WHERE b.operation_id=$1::uuid`},
  // The candidate version deliberately does not exist. P0141 must win
  // over a foreign-key violation; 23503 is not evidence of the node fence.
  {"exact_prepared_job_and_candidate_version",`
INSERT INTO config_apply_jobs(id,server_id,agent_id,config_version_id,action,status,request_payload)
SELECT b.apply_job_id,r.server_id,r.agent_id,b.candidate_version_id,
 'apply','pending','{"kind":"vless_credential_removal_v1"}'::jsonb
FROM vpn_credential_revocation_dispatch_bindings b
JOIN vpn_credential_revocations r ON r.id=b.operation_id
WHERE b.operation_id=$1::uuid`},
  {"unrelated_apply_job",`
INSERT INTO config_apply_jobs(server_id,agent_id,config_version_id,action,status,request_payload)
SELECT r.server_id,r.agent_id,r.baseline_version_id,'apply','pending','{}'::jsonb
FROM vpn_credential_revocations r WHERE r.id=$1::uuid`},
  {"advance_server_version_counter",`
UPDATE servers SET next_config_version=next_config_version+1
WHERE id=(SELECT server_id FROM vpn_credential_revocations WHERE id=$1::uuid)`},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   _,err:=f.pool.Exec(f.ctx,tc.query,f.operation)
   var pgerr *pgconn.PgError
   if !errors.As(err,&pgerr) || pgerr.Code!="P0141"{
    t.Fatalf("generic mutation bypassed reserved-node P0141 fence: %v",err)
   }
   if snapshot() != beforeState {
    t.Fatal("rejected generic mutation changed durable state")
   }
   if err:=f.svc.CheckPreparedRemovalDispatchAdmission(f.ctx,f.operation,f.nonce,true);err!=nil{
    t.Fatal("rejected mutation invalidated the original approval",err)
   }
  })
 }
 var afterVersions,afterJobs,afterNext int
 var afterActive string
 if err:=f.pool.QueryRow(f.ctx,`SELECT
  (SELECT count(*) FROM config_versions),
  (SELECT count(*) FROM config_apply_jobs),
  s.next_config_version,s.active_config_version_id::text
 FROM servers s JOIN vpn_credential_revocations r ON r.server_id=s.id
 WHERE r.id=$1::uuid`,f.operation).Scan(
 &afterVersions,&afterJobs,&afterNext,&afterActive);err!=nil{t.Fatal(err)}
 if beforeVersions!=afterVersions || beforeJobs!=afterJobs ||
  beforeNext!=afterNext || activeVersion!=afterActive{
  t.Fatal("a forbidden generic apply mutation changed the reserved node")
 }
 var prepared string
 if err:=f.pool.QueryRow(f.ctx,`SELECT state FROM vpn_credential_revocations
 WHERE id=$1::uuid`,f.operation).Scan(&prepared);err!=nil||prepared!="prepared"{
  t.Fatal("generic SQL attempt released original revocation reservation",err)
 }
 // Human approval and positive observation remain distinct from execution;
 // the Manager must not claim any credential-removal runtime success.
 var evidence int
 if err:=f.pool.QueryRow(f.ctx,`SELECT count(*)
 FROM vpn_credential_removal_dispatch_authorizations
 WHERE operation_id=$1::uuid`,f.operation).Scan(&evidence);err!=nil||evidence!=1{
  t.Fatal("rejected generic mutation changed durable human authority",err)
 }
}
