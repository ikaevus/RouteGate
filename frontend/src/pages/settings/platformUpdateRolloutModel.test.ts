import assert from 'node:assert/strict';
import test from 'node:test';
import type { PlatformUpdateRollout } from '../../entities/system/api/platformUpdateRolloutApi.ts';
import {
  appendSelectedServers,
  advanceFailureRequiresDurableRefresh,
  isCanonicalUuid,
  isCanonicalRouteGateVersion,
  moveSelectedServer,
  parseStoredRolloutCreateAttempt,
  rolloutNextAction,
  sameRolloutCreateRequest,
  type StoredRolloutCreateAttempt,
} from './platformUpdateRolloutModel.ts';

const serverA = '550e8400-e29b-41d4-a716-446655440001';
const serverB = '550e8400-e29b-41d4-a716-446655440002';

function rollout(status: PlatformUpdateRollout['status'], entryStatus: PlatformUpdateRollout['entries'][number]['status']): PlatformUpdateRollout {
  return {
    id: '550e8400-e29b-41d4-a716-446655440000',
    targetVersion: 'v1.2.3',
    status,
    createdAt: '2026-09-02T00:00:00Z',
    entries: [{ serverId: serverA, position: 0, status: entryStatus, planningBlockers: [] }],
  };
}

test('one explicit UI action maps to one controller step', () => {
  assert.equal(rolloutNextAction(rollout('pending', 'queued')), 'start');
  assert.equal(rolloutNextAction(rollout('running', 'updating')), 'check');
  assert.equal(rolloutNextAction(rollout('running', 'healthy')), 'continue');
  assert.equal(rolloutNextAction(rollout('succeeded', 'healthy')), null);
  assert.equal(rolloutNextAction(rollout('failed', 'failed')), null);
  assert.equal(rolloutNextAction(rollout('outcome_unknown', 'outcome_unknown')), null);
});

test('ambiguous advance failures require a durable refresh before another step', () => {
  assert.equal(advanceFailureRequiresDurableRefresh(), true);
  assert.equal(advanceFailureRequiresDurableRefresh(500), true);
  assert.equal(advanceFailureRequiresDurableRefresh(503), true);
  assert.equal(advanceFailureRequiresDurableRefresh(408), true);
  assert.equal(advanceFailureRequiresDurableRefresh(409), true);
  assert.equal(advanceFailureRequiresDurableRefresh(429), true);
  assert.equal(advanceFailureRequiresDurableRefresh(400), false);
  assert.equal(advanceFailureRequiresDurableRefresh(403), false);
  assert.equal(advanceFailureRequiresDurableRefresh(404), false);
});

test('resume IDs must use the canonical lowercase UUID form', () => {
  assert.equal(isCanonicalUuid(serverA), true);
  assert.equal(isCanonicalUuid(serverA.toUpperCase()), false);
  assert.equal(isCanonicalUuid(`{${serverA}}`), false);
});

test('ordered selection movement is bounded and immutable', () => {
  const original = [serverA, serverB];
  assert.deepEqual(moveSelectedServer(original, serverB, -1), [serverB, serverA]);
  assert.deepEqual(moveSelectedServer(original, serverA, -1), original);
  assert.deepEqual(original, [serverA, serverB]);
});

test('select all appends new nodes without destroying the chosen order', () => {
  assert.deepEqual(appendSelectedServers([serverB], [serverA, serverB]), [serverB, serverA]);
});

test('creation request identity includes target and exact server order', () => {
  const attempt: StoredRolloutCreateAttempt = {
    idempotencyKey: '550e8400-e29b-41d4-a716-446655440010',
    targetVersion: 'v1.2.3',
    serverIds: [serverA, serverB],
  };
  assert.equal(sameRolloutCreateRequest(attempt, 'v1.2.3', [serverA, serverB]), true);
  assert.equal(sameRolloutCreateRequest(attempt, 'v1.2.3', [serverB, serverA]), false);
  assert.equal(sameRolloutCreateRequest(attempt, 'v1.2.4', [serverA, serverB]), false);
});

test('stored ambiguous-create recovery accepts only the bounded canonical contract', () => {
  const valid = JSON.stringify({
    idempotencyKey: '550e8400-e29b-41d4-a716-446655440010',
    targetVersion: 'v1.2.3',
    serverIds: [serverA, serverB],
  });
  assert.deepEqual(parseStoredRolloutCreateAttempt(valid)?.serverIds, [serverA, serverB]);
  assert.equal(parseStoredRolloutCreateAttempt(valid.replace(serverB, serverA)), null);
  assert.equal(parseStoredRolloutCreateAttempt(valid.replace('41d4', '31d4')), null);
  assert.equal(parseStoredRolloutCreateAttempt('{not-json'), null);
});

test('target version validation mirrors the bounded Agent request language', () => {
  assert.equal(isCanonicalRouteGateVersion('v1.2.3'), true);
  assert.equal(isCanonicalRouteGateVersion('1.2.3-rc.1+build.7'), true);
  assert.equal(isCanonicalRouteGateVersion('dev'), false);
  assert.equal(isCanonicalRouteGateVersion(' v1.2.3'), false);
  assert.equal(isCanonicalRouteGateVersion(`v1.2.3-${'a'.repeat(240)}`), false);
});
