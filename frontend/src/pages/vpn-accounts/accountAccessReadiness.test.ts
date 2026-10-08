import assert from 'node:assert/strict';
import test from 'node:test';
import {
  accountAccessNextStep,
  mayIssueFreshAccess,
  type AccountAccessReadiness,
} from './accountAccessReadiness.ts';

test('new account must apply before the first device link is issued', () => {
  for (const status of ['awaiting_apply', 'awaiting_first_apply'] as const) {
    assert.equal(accountAccessNextStep(true, status, true), 'apply');
    assert.equal(mayIssueFreshAccess(status), false);
  }
});

test('unassigned, error and checking states never silently issue links', () => {
  const cases: [AccountAccessReadiness, 'assign_node' | 'investigate' | 'wait'][] = [
    ['unassigned', 'assign_node'],
    ['unavailable', 'investigate'],
    ['check_failed', 'investigate'],
    ['checking', 'wait'],
  ];
  for (const [status, expected] of cases) {
    assert.equal(accountAccessNextStep(true, status, true), expected);
    assert.equal(mayIssueFreshAccess(status), false);
  }
  assert.equal(accountAccessNextStep(false, 'ready', true), 'assign_node');
});

test('working applied access takes precedence over pending desired changes', () => {
  assert.equal(accountAccessNextStep(true, 'ready', true), 'add_device');
  assert.equal(mayIssueFreshAccess('ready'), true);
  assert.equal(accountAccessNextStep(true, 'ready', false), null);
});
