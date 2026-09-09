import test from 'node:test';
import assert from 'node:assert/strict';
import { managedSetInput, canEnableManagedSet, managedSourceHealth } from './managedRoutingModel.ts';

test('a failed initial download cannot enable routing, but a failed refresh retains a usable snapshot', () => {
  const pending = { lastSuccessAt: null, snapshotSha256: '', lastError: '' };
  assert.equal(managedSourceHealth(pending), 'pending');
  assert.equal(canEnableManagedSet(pending), false);
  assert.equal(managedSourceHealth({ ...pending, lastError: 'HTTP 503' }), 'failed');
  const good = { lastSuccessAt: '2026-09-09T00:00:00Z', snapshotSha256: 'a'.repeat(64), lastError: '' };
  assert.equal(managedSourceHealth(good), 'healthy');
  assert.equal(canEnableManagedSet({ ...good, snapshotSha256: '' }), false);
  assert.equal(managedSourceHealth({ ...good, lastError: 'invalid JSON' }), 'failed');
  assert.equal(canEnableManagedSet(good), true);
});
test('editing preserves source identity and precedence without submitting server-owned health fields', () => {
  const input = { name: 'RU Blocked', provider: 'custom', sourceUrl: 'https://example.org/rules.json', priority: 2000, action: 'vpn' as const, enabled: true, refreshHours: 24 };
  const source = { ...input, id: 'set', routingProfileId: 'profile', snapshotSha256: 'abc', lastSuccessAt: null, lastAttemptAt: null, lastError: '', createdAt: '', updatedAt: '' };
  assert.deepEqual(managedSetInput(source), input);
});
