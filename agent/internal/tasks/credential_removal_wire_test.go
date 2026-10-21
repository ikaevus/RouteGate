package tasks

import (
 "encoding/json"
 "strings"
 "testing"
)

// Manager and Agent are separate Go modules. The sample uses synthetic IDs;
// it asserts that the typed authenticated Agent task can receive the existing
// one-account-only VLESS revocation contract without any runtime mutation.
func TestCredentialRemovalAgentWireEnvelope(t *testing.T) {
 wire:=[]byte(`{
 "id":"44444444-4444-4444-8444-444444444444",
 "kind":"vless_credential_removal_v1",
 "serverId":"55555555-5555-4555-8555-555555555555",
 "agentId":"66666666-6666-4666-8666-666666666666",
 "configVersionId":"77777777-7777-4777-8777-777777777777",
 "action":"apply",
 "status":"in_progress",
 "configHash":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
 "credentialRemoval":{
  "schemaVersion":1,
  "operationId":"11111111-1111-4111-8111-111111111111",
  "accountId":"22222222-2222-4222-8222-222222222222",
  "baselineVersionId":"33333333-3333-4333-8333-333333333333",
  "baselineRuntimeHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "candidateRuntimeHash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "vlessUuid":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  "sharedProcessRestartAcknowledged":true
 }
}`)
 var task ConfigTask
 if err:=json.Unmarshal(wire,&task);err!=nil{t.Fatal(err)}
 if task.EffectiveKind()!=TaskKindCredentialRemoval || task.CredentialRemoval==nil ||
  task.CredentialRemoval.OperationID!="11111111-1111-4111-8111-111111111111" ||
  task.CredentialRemoval.AccountID!="22222222-2222-4222-8222-222222222222" ||
  task.CredentialRemoval.VLESSUUID!="aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" ||
  task.CredentialRemoval.CandidateRuntimeHash!=strings.Repeat("b",64) ||
  !task.CredentialRemoval.SharedProcessRestartAcknowledged{
  t.Fatal("Agent credential-removal wire contract missing a binding")
 }
 // A credential-removal task must NEVER accidentally fall through to generic
 // config staging: it requires its dedicated, fenced executor.
 if _,err:=NewStager(t.TempDir()).Stage(task);err==nil{
  t.Fatal("typed credential-removal task was accepted as ordinary config apply")
 }
 var plain ConfigTask
 if err:=json.Unmarshal([]byte(`{"id":"regular","status":"pending"}`),&plain);err!=nil{
  t.Fatal(err)
 }
 if plain.CredentialRemoval!=nil || plain.EffectiveKind()!=TaskKindConfigApply{
  t.Fatal("ordinary Agent task accidentally inherited removal payload")
 }
}
