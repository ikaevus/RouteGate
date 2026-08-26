package tasks

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestDetachedPlatformUpdateCommandIsFixedPolicy(t *testing.T) {
	taskID := "550e8400-e29b-41d4-a716-446655440000"
	path, argv, err := DetachedPlatformUpdateCommand(taskID); if err != nil { t.Fatalf("DetachedPlatformUpdateCommand: %v",err) }
	if path != "/usr/bin/systemd-run" { t.Fatalf("unexpected systemd-run path %q",path) }
	want := []string{"/usr/bin/systemd-run","--unit=routegate-vpn-update-"+taskID,"--collect","--no-block","--property=UMask=0077","--property=NoNewPrivileges=yes","/usr/local/bin/routegate-agent","--platform-update-worker-task="+taskID}
	if !reflect.DeepEqual(argv,want) { t.Fatalf("unexpected detached argv\n got: %#v\nwant: %#v",argv,want) }
	joined:=strings.Join(argv," "); for _,forbidden:=range []string{"http://","https://","--role","--bundle","--manifest","sh -c","bash -c"}{ if strings.Contains(joined,forbidden){t.Fatalf("detached command contains caller-controlled privileged selector %q: %s",forbidden,joined)} }
}
func TestDetachedPlatformUpdateCommandRejectsNonCanonicalIDs(t *testing.T){ for _,taskID:=range []string{"","not-a-uuid","550E8400-E29B-41D4-A716-446655440000","550e8400-e29b-11d4-a716-446655440000","550e8400-e29b-41d4-c716-446655440000","../../etc/passwd"}{ if _,_,err:=DetachedPlatformUpdateCommand(taskID);err==nil{t.Fatalf("expected task ID %q to be rejected",taskID)} } }
func TestCanonicalPlatformUpdateBundleNames(t *testing.T){ for _,name:=range []string{"routegate-v1.2.3-linux-amd64.tar.gz","routegate-1.2.3-rc.1-linux-arm64.tar.gz"}{ if !isCanonicalPlatformUpdateBundleName(name){t.Fatalf("expected canonical bundle %q",name)}; version,err:=platformUpdateVersionFromBundle(name);if err!=nil||version==""{t.Fatalf("expected version from bundle %q, got %q err=%v",name,version,err)} }; for _,name:=range []string{"routegate-v1.2.3-linux-386.tar.gz","../routegate-v1.2.3-linux-amd64.tar.gz","routegate--linux-amd64.tar.gz","evil-routegate-v1.2.3-linux-amd64.tar.gz"}{if isCanonicalPlatformUpdateBundleName(name){t.Fatalf("expected non-canonical bundle %q to be rejected",name)}} }

func TestPreparedReceiptIsRequiredAndVersionBound(t *testing.T){
	store:=testPlatformUpdateReceiptStore(t); taskID:="550e8400-e29b-41d4-a716-446655440000"
	if err:=acceptPreparedPlatformUpdateReceipt(store,taskID,"v1.2.3");err==nil{t.Fatal("worker accepted missing prepared receipt")}
	if _,err:=store.CreatePrepared(taskID,"v1.2.3");err!=nil{t.Fatal(err)}
	if err:=acceptPreparedPlatformUpdateReceipt(store,taskID,"v1.2.4");err==nil{t.Fatal("worker accepted mismatched prepared version")}
	if err:=acceptPreparedPlatformUpdateReceipt(store,taskID,"v1.2.3");err!=nil{t.Fatalf("matching prepared receipt rejected: %v",err)}
}
func TestExistingStartedReceiptReconcilesAndRefusesReplay(t *testing.T){ store:=testPlatformUpdateReceiptStore(t);taskID:="550e8400-e29b-41d4-a716-446655440000";if _,err:=store.CreatePrepared(taskID,"v1.2.3");err!=nil{t.Fatal(err)};if _,err:=store.MarkMutationStarted(taskID);err!=nil{t.Fatal(err)};if err:=acceptPreparedPlatformUpdateReceipt(store,taskID,"v1.2.3");err==nil{t.Fatal("orphaned mutation_started receipt was allowed to replay")};receipt,err:=store.Read(taskID);if err!=nil{t.Fatal(err)};if receipt.Phase!=PlatformUpdateReceiptOutcomeUnknown||receipt.Code!="agent_restart_after_mutation_started"{t.Fatalf("unexpected reconciled receipt: %+v",receipt)} }
func TestTerminalReceiptRefusesReplay(t *testing.T){store:=testPlatformUpdateReceiptStore(t);taskID:="550e8400-e29b-41d4-a716-446655440000";if _,err:=store.CreatePrepared(taskID,"v1.2.3");err!=nil{t.Fatal(err)};if _,err:=store.MarkMutationStarted(taskID);err!=nil{t.Fatal(err)};if _,err:=store.MarkSucceeded(taskID);err!=nil{t.Fatal(err)};if err:=acceptPreparedPlatformUpdateReceipt(store,taskID,"v1.2.3");err==nil{t.Fatal("terminal receipt was allowed to replay")} }
func TestPlatformUpdateWaitOutcomeClassification(t *testing.T){
	signaledErr:=exec.Command("sh","-c","kill -KILL $$").Run();if signaledErr==nil||!platformUpdateWaitOutcomeUnknown(signaledErr){t.Fatalf("signaled updater must be outcome_unknown, err=%v",signaledErr)}
	rollbackErr:=exec.Command("sh","-c","exit 75").Run();if rollbackErr==nil||!platformUpdateWaitOutcomeUnknown(rollbackErr){t.Fatalf("incomplete rollback must be outcome_unknown, err=%v",rollbackErr)};if code:=platformUpdateOutcomeUnknownCode(rollbackErr);code!="rollback_incomplete"{t.Fatalf("rollback outcome code=%q",code)}
	exitErr:=exec.Command("sh","-c","exit 7").Run();if exitErr==nil||platformUpdateWaitOutcomeUnknown(exitErr){t.Fatalf("normal nonzero updater exit must be deterministic failure, err=%v",exitErr)}
	if !platformUpdateWaitOutcomeUnknown(exec.ErrNotFound){t.Fatal("unclassified wait error must fail closed to outcome_unknown")}
}
