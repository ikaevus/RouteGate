import assert from 'node:assert/strict';
import test from 'node:test';
import { groupConnections } from './groupConnections.ts';
import type { ClientConnection } from '../api/connectionApi.ts';

const base: ClientConnection = {
  vpnAccountId: 'a', accountName: 'Felix', serverId: 's', serverName: 'server',
  agentId: 'n', protocol: 'vless-reality', state: 'online', confidence: 'exact',
  connectionCount: 8, source: 'native', observedAt: '2026-09-08T12:00:00Z',
};

test('groups protocols without mutating input and retains details', () => {
  const items = [base, { ...base, protocol: 'shadowsocks', connectionCount: 1 }];
  const groups = groupConnections(items);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].connectionCount, 9);
  assert.deepEqual(groups[0].details, items);
  assert.equal(base.connectionCount, 8);
});

test('keeps account, server and node boundaries even for identical names', () => {
  assert.equal(groupConnections([base, { ...base, vpnAccountId: 'b' },
    { ...base, serverId: 'other' }, { ...base, agentId: 'other' }]).length, 4);
});

test('recent evidence does not inflate online counts; newest activity is retained', () => {
  const recent: ClientConnection = { ...base, protocol: 'shadowsocks', state: 'recently_active',
    confidence: 'heuristic', connectionCount: 20, lastActivityAt: '2026-09-08T12:01:00Z' };
  const [group] = groupConnections([recent, base]);
  assert.equal(group.state, 'online');
  assert.equal(group.confidence, 'exact');
  assert.equal(group.connectionCount, 8);
  assert.equal(group.lastActivityAt, recent.lastActivityAt);
  assert.equal(groupConnections([recent])[0].connectionCount, 0);
  assert.deepEqual(groupConnections([]), []);
});
