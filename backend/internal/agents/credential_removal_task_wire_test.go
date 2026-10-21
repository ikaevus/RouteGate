package agents_test

import (
 "encoding/json"
 "reflect"
 "strings"
 "testing"

 "github.com/ikaevus/routegate/backend/internal/agents"
 "github.com/ikaevus/routegate/backend/internal/revocations"
)

func TestAgentCredentialRemovalRequestWireParity(t *testing.T) {
 secret:="aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" // synthetic UUID only
 manager:=revocations.CredentialRemovalRequest{
  SchemaVersion:1,
  OperationID:"11111111-1111-4111-8111-111111111111",
  AccountID:"22222222-2222-4222-8222-222222222222",
  BaselineVersionID:"33333333-3333-4333-8333-333333333333",
  BaselineRuntimeHash:strings.Repeat("a",64),
  CandidateRuntimeHash:strings.Repeat("b",64),
  VLESSUUID:secret,
  SharedProcessRestartAcknowledged:true,
 }
 raw,err:=json.Marshal(manager)
 if err!=nil{t.Fatal(err)}
 var agent agents.AgentCredentialRemovalRequest
 if err=json.Unmarshal(raw,&agent);err!=nil{t.Fatal(err)}
 roundtrip,err:=json.Marshal(agent)
 if err!=nil||!reflect.DeepEqual(json.RawMessage(roundtrip),json.RawMessage(raw)){
  t.Fatal("Manager task request drifted from pinned revocation contract",err)
 }
 if agent.SchemaVersion!=1 || agent.VLESSUUID!=secret ||
  agent.CandidateRuntimeHash!=manager.CandidateRuntimeHash ||
  !agent.SharedProcessRestartAcknowledged {
  t.Fatal("Agent wire lost a mandatory VLESS revocation binding")
 }
 task:=agents.AgentConfigTask{
  ID:"44444444-4444-4444-8444-444444444444",
  ServerID:"55555555-5555-4555-8555-555555555555",
  AgentID:"66666666-6666-4666-8666-666666666666",
  ConfigVersionID:"77777777-7777-4777-8777-777777777777",
  Kind:revocations.TaskKindCredentialRemoval,Action:"apply",
  Status:"in_progress",ConfigHash:strings.Repeat("c",64),
 }
 delivered,err:=json.Marshal(agents.AgentNextTaskResponse{Task:&agents.AgentDeliveryTask{
  AgentConfigTask:task,
  CredentialRemoval:&agent,
 }})
 if err!=nil{t.Fatal(err)}
 var decoded agents.AgentNextTaskResponse
 if err=json.Unmarshal(delivered,&decoded);err!=nil||decoded.Task==nil||
  decoded.Task.Kind!=revocations.TaskKindCredentialRemoval||
  !reflect.DeepEqual(decoded.Task.CredentialRemoval,&agent){
  t.Fatal("Manager Agent-only delivery DTO lost the typed removal request",err)
 }
 adminWire,err:=json.Marshal(task)
 if err!=nil || strings.Contains(string(adminWire),"credentialRemoval") ||
  strings.Contains(string(adminWire),secret){
  t.Fatal("shared operator-facing job DTO leaked a revocation credential",err)
 }
 // Generic config apply must NOT carry a stale or fabricated UUID by default.
 regular,err:=json.Marshal(agents.AgentConfigTask{Kind:agents.AgentTaskKindConfigApply})
 if err!=nil||strings.Contains(string(regular),"credentialRemoval")||strings.Contains(string(regular),secret){
  t.Fatal("ordinary config task exposed credential removal material",err)
 }
}
