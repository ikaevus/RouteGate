#!/usr/bin/env python3
"""Approved first FI->US canary leg via normal Manager API; never source cleanup."""
import json,os,re,subprocess,time,urllib.error,urllib.request
ACCOUNT='83ef4f8b-78a6-474b-8dd7-c32c1b71fe66'
SOURCE='da1b3f03-b017-4170-8da6-dc4314e5d7ad'
TARGET='04a9d5db-1dfe-461a-8ddd-fe94a0480a0b'
BASE='http://127.0.0.1:8080/api/v1'
class Stop(Exception):pass

def sql(statement):
    env=os.environ.copy();env['PGOPTIONS']='-c default_transaction_read_only=on -c statement_timeout=30s -c lock_timeout=3s'
    p=subprocess.run(['psql',os.environ['ROUTEGATE_DATABASE_URL'],'-X','-qAt','-v','ON_ERROR_STOP=1','-c',statement],env=env,capture_output=True,text=True)
    if p.returncode:raise Stop('database_check_failed (private details suppressed)')
    return p.stdout.strip()

def identities():
    return sql("SELECT md5(jsonb_build_object('others',(SELECT jsonb_agg(jsonb_build_array(id,server_id,status,vless_uuid) ORDER BY id) FROM vpn_accounts WHERE id<>'"+ACCOUNT+"'::uuid),'tokens',(SELECT jsonb_agg(to_jsonb(t)-ARRAY['last_used_at','updated_at'] ORDER BY id) FROM vpn_subscription_tokens t),'devices',(SELECT jsonb_agg(to_jsonb(d)-ARRAY['last_used_at','last_seen_at','updated_at'] ORDER BY id) FROM vpn_account_devices d))::text)")

def request(path,payload=None,token=None):
    h={'User-Agent':'RouteGate approved RG140 canary','Content-Type':'application/json'}
    if token:h['Authorization']='Bearer '+token
    req=urllib.request.Request(BASE+path,data=None if payload is None else json.dumps(payload).encode(),headers=h)
    try:
        with urllib.request.urlopen(req,timeout=45) as r:return json.load(r)
    except urllib.error.HTTPError as e:
        try:v=json.load(e)
        except Exception:v={}
        code=v.get('error',{}).get('code','') if isinstance(v.get('error'),dict) else v.get('status','')
        message=v.get('error',{}).get('message','') if isinstance(v.get('error'),dict) else v.get('message','')
        reason=re.search(r'transfer safety gate blocked the action: ([a-z_]+)',message)
        raise Stop('api_http_'+str(e.code)+' '+str(code)+((' '+reason.group(1)) if reason else '')) from None
    except Exception:raise Stop('api_unreachable_or_invalid_response') from None

def run():
    if sql('SELECT max(version) FROM schema_migrations')!='000159_staged_account_transfers':raise Stop('candidate_schema_missing')
    account=sql("SELECT display_name||'|'||server_id||'|'||status FROM vpn_accounts WHERE id='"+ACCOUNT+"'::uuid")
    if account!='fi-test|'+SOURCE+'|active':raise Stop('fi_test_baseline_does_not_match')
    login=os.getenv('ROUTEGATE_BOOTSTRAP_ADMIN_USERNAME') or os.getenv('ROUTEGATE_BOOTSTRAP_ADMIN_EMAIL')
    password=os.getenv('ROUTEGATE_BOOTSTRAP_ADMIN_PASSWORD')
    if not login or not password:raise Stop('api_login_credentials_unavailable; authenticate in Manager UI')
    token=request('/auth/login',{'login':login,'password':password})['token']
    try:
        version=request('/system/version',token=token)
        if version.get('manager',{}).get('gitCommit')!='d2a0990e91703a4b6e8744cab172b0d092953110':raise Stop('candidate_build_does_not_match')
        baseline=identities()
        path='/vpn-accounts/'+ACCOUNT
        before=request(path+'/client-connection',token=token)
        if not before['endpoint'].endswith(':443'):raise Stop('source_applied_port_does_not_match')
        latest=request(path+'/transfer',token=token).get('transfer')
        if latest and not latest.get('completedAt'):raise Stop('existing_transfer_requires_resume')
        tr=request(path+'/transfer',{'targetServerId':TARGET},token)
        if tr['sourceServerId']!=SOURCE or tr['targetServerId']!=TARGET:raise Stop('transfer_scope_changed')
        print(json.dumps({k:tr[k] for k in ('id','state','sourceVersionId','targetVersionId','targetJobId')}),flush=True)
        # While Agent applies the overlay, the canonical connection remains FI.
        staged=request(path+'/client-connection',token=token)
        if staged['endpoint']!=before['endpoint'] or staged.get('vlessLink')!=before.get('vlessLink'):raise Stop('source_connection_changed_before_cutover')
        print('source_connection_during_preparation=unchanged',flush=True)
        deadline=time.monotonic()+240
        while True:
            status=sql("SELECT status FROM config_apply_jobs WHERE id='"+tr['targetJobId']+"'::uuid")
            if status=='succeeded':break
            if status not in ('pending','in_progress'):raise Stop('target_apply_'+status)
            if time.monotonic()>deadline:raise Stop('target_apply_wait_timeout; source retained')
            time.sleep(3)
        tr=request(path+'/transfer/'+tr['id'],{'action':'verify','confirmed':False},token)
        if tr['state']!='target_ready':raise Stop('target_verification_failed')
        if identities()!=baseline:raise Stop('unrelated_account_or_subscription_identity_changed')
        tr=request(path+'/transfer/'+tr['id'],{'action':'cutover','confirmed':False},token)
        after=request(path+'/client-connection',token=token)
        if tr['state']!='client_refresh_pending' or not after['endpoint'].endswith(':8443'):raise Stop('cutover_response_does_not_match')
        retained=sql("SELECT cv.client_settings->'accounts' ? '"+ACCOUNT+"' FROM servers s JOIN config_versions cv ON cv.id=s.active_config_version_id WHERE s.id='"+SOURCE+"'::uuid")
        if retained!='t':raise Stop('source_membership_not_retained')
        if identities()!=baseline:raise Stop('unrelated_account_or_subscription_identity_changed')
        print(json.dumps({'id':tr['id'],'state':tr['state'],'endpoint':after['endpoint'],'sourceMembershipRetained':True,'identitiesUnchanged':True,'devices':[{'id':d['id'],'name':d['name']} for d in tr['devices']]}),flush=True)
    finally:
        try:request('/auth/logout',{},token)
        except Stop:print('temporary_api_session_logout_failed',flush=True)

if __name__=='__main__':
    try:run()
    except Stop as e:print('CANARY_BLOCKED: '+str(e),flush=True);raise SystemExit(3)
