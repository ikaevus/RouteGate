import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// Node's direct TS runner doesn't resolve Vite-only extensionless imports;
// this copy contract test checks the exact source shipped to Vite. The real
// account status/dialog behavior is verified in the isolated browser+Manager
// PostgreSQL workspace integration job.
const source = readFileSync(new URL('./vpnAccountManagementCopy.ts', import.meta.url), 'utf8');

test('all sensitive single and bulk status actions have a real consequence confirmation', () => {
  for (const field of [
    'activateConfirm', 'suspendConfirm', 'revokeConfirm', 'deleteConfirm',
    'bulkConfirmActivate', 'bulkConfirmSuspend', 'bulkConfirmRevoke', 'bulkConfirmDelete',
  ]) {
    const count = source.split(new RegExp(`\\b${field}:`, 'g')).length - 1;
    assert.equal(count, 2, `expected both RU and EN ${field}`);
  }
});

test('both locales explicitly distinguish account record status from runtime VPN revocation', () => {
  for (const field of ['runtimeNotConfirmed', 'statusRecordedPending', 'deletedRuntimeNotConfirmed', 'verifyNodeAccess']) {
    assert.equal(source.split(new RegExp(`\\b${field}:`, 'g')).length - 1, 2, `missing RU/EN ${field}`);
  }
  for (const fragment of [
    'НЕ немедленное отключение VPN', 'not an immediate VPN disconnect',
    'отдельного подтверждённого применения', 'separately confirmed node configuration apply',
    'не подтверждён', 'not verified', 'still-active subscription links',
    'ссылки подписки снова смогут',
  ]) {
    assert.ok(source.toLowerCase().includes(fragment.toLowerCase()), `missing safety explanation: ${fragment}`);
  }
  assert.ok(source.includes('\x5cn\x5cn'), 'dialog copy must separate action and consequences');
});
