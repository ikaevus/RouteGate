#!/usr/bin/env python3
"""One real staging failure for fi-test rollback on US. Database access is read-only."""
import hashlib
import json
import os
import pathlib
import re
import signal
import stat
import subprocess
import sys
import time

ACCOUNT = '83ef4f8b-78a6-474b-8dd7-c32c1b71fe66'
FI = 'da1b3f03-b017-4170-8da6-dc4314e5d7ad'
US = '04a9d5db-1dfe-461a-8ddd-fe94a0480a0b'
STAGING = pathlib.Path('/var/lib/routegate-agent/configs')
UUID = re.compile(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}')
PROTECTED = ['/etc/routegate', '/etc/sing-box', '/etc/wireguard', '/etc/hysteria',
             '/etc/routegate-mtproto', '/etc/nginx', '/usr/local/bin/routegate-manager',
             '/usr/local/bin/routegate-agent']
SERVICES = ['routegate-manager', 'routegate-agent', 'sing-box', 'nginx',
            'wg-quick@routegate-wg0', 'hysteria-server', 'routegate-mtproto']

class Stop(Exception):
    pass

def command(args):
    result = subprocess.run(args, capture_output=True, text=True)
    if result.returncode:
        raise Stop('command_failed_private_details_suppressed')
    return result.stdout.strip()

def sql(statement):
    env = os.environ.copy()
    env['PGOPTIONS'] = '-c default_transaction_read_only=on -c statement_timeout=3s -c lock_timeout=1s'
    env['PGCONNECT_TIMEOUT'] = '3'
    result = subprocess.run(['psql', os.environ['ROUTEGATE_DATABASE_URL'], '-X', '-qAt',
                             '-v', 'ON_ERROR_STOP=1', '-c', statement],
                            env=env, capture_output=True, text=True)
    if result.returncode:
        raise Stop('readonly_database_check_failed')
    return result.stdout.strip()

def snapshot():
    files = {}
    for name in PROTECTED:
        root = pathlib.Path(name)
        paths = [root] if root.is_file() else sorted(root.rglob('*')) if root.is_dir() else []
        for path in paths:
            if path.is_symlink():
                files[str(path)] = ('link', os.readlink(path))
            elif path.is_file():
                with path.open('rb') as stream:
                    files[str(path)] = hashlib.file_digest(stream, 'sha256').hexdigest()
    services = {name: command(['systemctl', 'show', name, '-p', 'MainPID', '-p', 'ActiveState',
                               '-p', 'ActiveEnterTimestampMonotonic']) for name in SERVICES}
    return files, services

def operation():
    row = sql("SELECT jsonb_build_object('id',t.id,'state',t.state,'cutover',t.cutover_at IS NOT NULL,"
              "'cleanup',t.cleanup_version_id,'job',t.cleanup_job_id,'placement',a.server_id)::text "
              "FROM vpn_account_transfers t JOIN vpn_accounts a ON a.id=t.vpn_account_id "
              f"WHERE t.vpn_account_id='{ACCOUNT}' AND t.source_server_id='{FI}' "
              f"AND t.target_server_id='{US}' AND t.completed_at IS NULL")
    try:
        return json.loads(row)
    except (ValueError, TypeError):
        raise Stop('exact_active_canary_unavailable') from None

def only_canary_job(job=None):
    condition = f"AND id<>'{job}'::uuid" if job else ''
    if sql("SELECT NOT EXISTS(SELECT 1 FROM config_apply_jobs WHERE status IN ('pending','in_progress') "
           + condition + ") AND NOT EXISTS(SELECT 1 FROM agent_operation_jobs WHERE status IN ('pending','in_progress')) "
           "AND NOT EXISTS(SELECT 1 FROM agent_platform_update_jobs WHERE status IN ('pending','in_progress'))") != 't':
        raise Stop('other_work_is_pending')

def job_result(version, job):
    row = sql("SELECT jsonb_build_object('status',status,'expected_node',server_id="
              f"'{US}'::uuid,'error',error_message)::text FROM config_apply_jobs WHERE "
              f"id='{job}'::uuid AND config_version_id='{version}'::uuid")
    try:
        return json.loads(row)
    except (ValueError, TypeError):
        raise Stop('exact_cleanup_job_unavailable') from None

def install_fault(version):
    if not UUID.fullmatch(version):
        raise Stop('invalid_version')
    if any(parent.is_symlink() for parent in [STAGING, *STAGING.parents]):
        raise Stop('staging_symlink_refused')
    if not STAGING.is_dir():
        raise Stop('staging_directory_unavailable')
    path = STAGING / (version + '.json.tmp')
    # Never touch an existing file or directory, even if the task raced us.
    try:
        path.mkdir(mode=0o700)
    except FileExistsError:
        raise Stop('task_already_staged_or_fault_exists') from None
    return path, path.stat().st_ino

def remove_fault(path, inode):
    try:
        info = path.lstat()
    except FileNotFoundError:
        return
    if info.st_ino != inode or not stat.S_ISDIR(info.st_mode):
        raise Stop('fault_identity_changed_manual_recovery_required')
    path.rmdir()  # Only our exact empty directory; never recursively remove.

def run():
    cfg = pathlib.Path('/etc/routegate/agent.yaml').read_text()
    value = re.search(r'^config_staging_dir:\s*(.*?)\s*$', cfg, re.M)
    configured = value.group(1).strip('"\'') if value else str(STAGING)
    if configured != str(STAGING):
        raise Stop('custom_staging_layout_refused')
    initial = operation()
    if initial['state'] != 'client_refresh_pending' or not initial['cutover'] or initial['placement'] != US:
        raise Stop('requires_fi_to_us_cutover_without_source_cleanup')
    only_canary_job()
    baseline = snapshot()
    print('armed=fi_test_US_target_cleanup_only; press_restore_source_not_remove_source', flush=True)
    deadline = time.monotonic() + 180
    while time.monotonic() < deadline:
        current = operation()
        if current['id'] != initial['id']:
            raise Stop('operation_changed')
        if current['state'] == 'target_cleaning' and current['placement'] == FI:
            break
        if current['state'] != 'client_refresh_pending':
            raise Stop('unexpected_operation_state')
        time.sleep(0.1)
    else:
        raise Stop('no_rollback_requested_within_three_minutes')
    version, job = current['cleanup'], current['job']
    if not all(isinstance(x, str) and UUID.fullmatch(x) for x in (version, job)):
        raise Stop('cleanup_identity_unavailable')
    only_canary_job(job)
    evidence = job_result(version, job)
    if evidence['status'] != 'pending' or not evidence['expected_node']:
        raise Stop('missed_pending_job_no_fault_installed')
    path, inode = install_fault(version)
    unit = 'routegate-rg140-fault-' + version
    try:
        # Independent recovery even if SSH or this helper is terminated abruptly.
        command(['systemd-run', '--quiet', '--unit=' + unit, '--on-active=120s',
                 '/usr/bin/rmdir', '--', str(path)])
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            evidence = job_result(version, job)
            if evidence['status'] in ('failed', 'succeeded'):
                break
            time.sleep(0.25)
        else:
            raise Stop('task_timeout_fault_will_be_removed')
        if evidence['status'] != 'failed' or 'write staged config' not in (evidence['error'] or ''):
            raise Stop('failure_not_observed_or_race_no_result_claimed')
        if snapshot() != baseline:
            raise Stop('protected_files_or_service_state_changed')
        print('cleanup_job=real_agent_staging_failure; running_configs_and_services=unchanged', flush=True)
    finally:
        remove_fault(path, inode)
        # Cancel only our recovery timer after successful removal.
        subprocess.run(['systemctl', 'stop', unit + '.timer'], capture_output=True)
        print('temporary_fault=removed; normal_Manager_retry_is_available', flush=True)

def interrupted(_signum, _frame):
    raise Stop('interrupted')

if __name__ == '__main__':
    for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(sig, interrupted)
    try:
        run()
    except Exception as error:
        print('CLEANUP_TEST_STOPPED: ' + (str(error) if isinstance(error, Stop) else 'private_details_suppressed'), flush=True)
        sys.exit(1)
