import assert from 'node:assert/strict';
import test from 'node:test';
import { subscriptionRequestEvidence } from './subscriptionRequestEvidence.ts';

test('current active subscription token request is evidence of a fetch, never a tunnel', () => {
  assert.equal(subscriptionRequestEvidence({ hasActiveToken: true, tokenLastUsedAt: '2026-10-08T12:30:00Z' }), 'observed');
});

test('no active link never reports a request even if old device usage exists', () => {
  assert.equal(subscriptionRequestEvidence({ hasActiveToken: false, tokenLastUsedAt: '2026-10-08T12:30:00Z' }), 'no_active_link');
  assert.equal(subscriptionRequestEvidence({ hasActiveToken: false }), 'no_active_link');
});

test('new or rotated bearer with no request stays unobserved', () => {
  assert.equal(subscriptionRequestEvidence({ hasActiveToken: true, tokenLastUsedAt: null }), 'not_observed');
  assert.equal(subscriptionRequestEvidence({ hasActiveToken: true }), 'not_observed');
  assert.equal(subscriptionRequestEvidence({ hasActiveToken: true, tokenLastUsedAt: '' }), 'not_observed');
});

test('unparseable server value is unknown, not proven or never requested', () => {
  assert.equal(subscriptionRequestEvidence({ hasActiveToken: true, tokenLastUsedAt: 'unavailable' }), 'unknown');
});
