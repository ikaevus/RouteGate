import assert from 'node:assert/strict';
import test from 'node:test';
import { getVpnAccountManagementCopy } from './vpnAccountManagementCopy.ts';

test('EN account lifecycle never equates a database status change with confirmed VPN disconnect', () => {
  const copy = getVpnAccountManagementCopy();
  for (const prompt of [
    copy.revokeConfirm('Test account'),
    copy.suspendConfirm('Test account'),
    copy.bulkConfirmRevoke(3),
    copy.bulkConfirmSuspend(3),
  ]) {
    assert.match(prompt, /subscription|subscriptions/i);
    assert.match(prompt, /VPN.*(connect|credential|work)|credentials.*working/i);
    assert.ok(prompt.includes('\n'), 'destructive state change should show separated consequences');
  }
  assert.match(copy.runtimeNotConfirmed, /not verified/i);
  assert.match(copy.statusRecordedPending, /NOT confirmed/);
  assert.match(copy.activateConfirm('Test account'), /still-active subscription links/);
  assert.match(copy.bulkConfirmActivate(2), /still-valid subscription URLs/);
  assert.match(copy.deleteConfirm('Test account'), /NOT prove/);
  assert.match(copy.bulkConfirmDelete(3), /NOT prove/);
  assert.match(copy.deletedRuntimeNotConfirmed, /NOT verified/);
});

test('RU account lifecycle warns about old imported credentials and distinguishes node apply', () => {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, 'window');
  Object.defineProperty(globalThis, 'window', {
    configurable: true, value: { localStorage: { getItem: () => 'ru' } },
  });
  try {
    const copy = getVpnAccountManagementCopy();
    assert.match(copy.revokeConfirm('Тест'), /НЕ немедленное отключение/);
    assert.match(copy.suspendConfirm('Тест'), /VPN-параметры могут работать/);
    assert.match(copy.activateConfirm('Тест'), /ссылки подписки снова смогут/);
    assert.match(copy.bulkConfirmRevoke(2), /не отключает действующие соединения/);
    assert.match(copy.bulkConfirmDelete(2), /не подтверждает удаление/);
    assert.match(copy.runtimeNotConfirmed, /не подтверждён/);
    assert.match(copy.statusRecordedPending, /НЕ подтверждено/);
  } finally {
    if (descriptor) Object.defineProperty(globalThis, 'window', descriptor);
    else Reflect.deleteProperty(globalThis, 'window');
  }
});
