import assert from 'node:assert/strict';
import test from 'node:test';
import type { ProtocolSettingsResponse } from '../../entities/server/api/serverApi.ts';
import {
  nodeCompletedSteps,
  nodeSetupStage,
  protocolConfigured,
  selectGettingStartedNode,
  type NodeSetupFacts,
} from './gettingStartedModel.ts';

function node(overrides: Partial<NodeSetupFacts> & Pick<NodeSetupFacts, 'id' | 'name'>): NodeSetupFacts {
  return {
    agentOnline: true,
    coreInstalled: true,
    protocolConfigured: true,
    activeAccountCount: 0,
    access: 'not_served',
    servedAccountId: null,
    pendingAccountId: null,
    pendingStatus: null,
    pendingMessage: null,
    latestApplyFailed: false,
    latestApplyError: null,
    ...overrides,
  };
}

// The production situation: US serves its accounts from an applied
// configuration, RU is a configured test node without accounts, FI is a
// registered node without a connected Agent.
const us = node({
  id: '11111111-1111-4111-8111-111111111111', name: 'us.routegate.org',
  activeAccountCount: 3, access: 'served', servedAccountId: 'acc-us-1',
});
const ru = node({ id: '22222222-2222-4222-8222-222222222222', name: 'ru.routegate.org' });
const fi = node({
  id: '33333333-3333-4333-8333-333333333333', name: 'fi.routegate.org',
  agentOnline: false, coreInstalled: false, protocolConfigured: false,
});

function permutations<T>(items: T[]): T[][] {
  if (items.length <= 1) return [items];
  return items.flatMap((item, index) =>
    permutations([...items.slice(0, index), ...items.slice(index + 1)]).map((rest) => [item, ...rest]));
}

test('A: a new node without runtime or applied configuration is guided step by step', () => {
  assert.equal(nodeSetupStage(node({ id: 'n', name: 'n', agentOnline: false, coreInstalled: false, protocolConfigured: false })), 'connect');
  assert.equal(nodeSetupStage(node({ id: 'n', name: 'n', coreInstalled: false, protocolConfigured: false })), 'core');
  assert.equal(nodeSetupStage(node({ id: 'n', name: 'n', protocolConfigured: false })), 'protocol');
  assert.equal(nodeSetupStage(node({ id: 'n', name: 'n' })), 'account');
  assert.equal(nodeSetupStage(node({ id: 'n', name: 'n', activeAccountCount: 1, pendingAccountId: 'a', pendingStatus: 'awaiting_first_apply' })), 'apply');
  const selection = selectGettingStartedNode([node({ id: 'n', name: 'n', coreInstalled: false, protocolConfigured: false })]);
  assert.equal(selection.setupComplete, false);
  assert.equal(selection.focus?.id, 'n');
  assert.equal(nodeCompletedSteps(selection.focus!), 2);
});

test('B: one node serving an account from its applied configuration completes first-run setup', () => {
  const selection = selectGettingStartedNode([us]);
  assert.equal(selection.setupComplete, true);
  assert.equal(selection.focus?.id, us.id);
  assert.equal(nodeCompletedSteps(us), 5);
});

test('C: working US + empty RU + unconnected FI is complete in every API order', () => {
  for (const order of permutations([us, ru, fi])) {
    const selection = selectGettingStartedNode(order);
    assert.equal(selection.setupComplete, true, order.map((item) => item.name).join(','));
    assert.equal(selection.focus?.id, us.id);
    assert.deepEqual(selection.workingNodes.map((item) => item.id), [us.id]);
    assert.deepEqual(selection.otherNodes.map((item) => item.name), ['fi.routegate.org', 'ru.routegate.org']);
  }
});

test('C: without a working node the furthest node is chosen independently of the API order', () => {
  const draft = node({ id: '44444444-4444-4444-8444-444444444444', name: 'de.routegate.org', protocolConfigured: false });
  for (const order of permutations([ru, fi, draft])) {
    const selection = selectGettingStartedNode(order);
    assert.equal(selection.setupComplete, false);
    assert.equal(selection.focus?.id, ru.id);
  }
  // Equal progress: the node name decides, not the position in the response.
  const twin = node({ id: '55555555-5555-4555-8555-555555555555', name: 'ab.routegate.org' });
  for (const order of permutations([ru, twin])) {
    assert.equal(selectGettingStartedNode(order).focus?.id, twin.id);
  }
});

test('D: an account created after the last apply is not counted as deployed', () => {
  const pending = node({
    id: 'n', name: 'n', activeAccountCount: 1, access: 'not_served',
    pendingAccountId: 'acc-new', pendingStatus: 'awaiting_apply',
  });
  assert.equal(nodeSetupStage(pending), 'apply');
  assert.equal(selectGettingStartedNode([pending]).setupComplete, false);
});

test('D: an old served account behind three newer pending accounts keeps the node ready', () => {
  // The Manager evaluates every active account; the summary reports the old
  // served one although the three newest await the next apply.
  const mixed = node({
    id: 'n', name: 'us.routegate.org', activeAccountCount: 4, access: 'served', servedAccountId: 'acc-old',
  });
  assert.equal(nodeSetupStage(mixed), 'ready');
  const selection = selectGettingStartedNode([mixed, ru]);
  assert.equal(selection.setupComplete, true);
  assert.equal(selection.focus?.servedAccountId, 'acc-old');
});

test('E: saved settings with a failed apply are not treated as applied, and a working node stays working', () => {
  const neverApplied = node({
    id: 'n', name: 'n', activeAccountCount: 1, pendingAccountId: 'a', pendingStatus: 'awaiting_first_apply',
    latestApplyFailed: true, latestApplyError: 'healthcheck failed',
  });
  assert.equal(nodeSetupStage(neverApplied), 'apply');
  assert.equal(selectGettingStartedNode([neverApplied]).setupComplete, false);
  // A failed re-apply after changed settings keeps the previously applied,
  // still served configuration: the node remains the working node.
  const previouslyWorking = { ...us, protocolConfigured: false, latestApplyFailed: true, latestApplyError: 'validate failed' };
  assert.equal(nodeSetupStage(previouslyWorking), 'ready');
  assert.equal(selectGettingStartedNode([previouslyWorking]).setupComplete, true);
});

test('F: stages reached on different nodes are never combined into a working VPN', () => {
  const accountsButNoApply = node({ id: 'a', name: 'a', activeAccountCount: 2, pendingAccountId: 'x', pendingStatus: 'awaiting_first_apply' });
  const configuredButNoAccounts = node({ id: 'b', name: 'b' });
  const servedButOffline = node({ id: 'c', name: 'c', agentOnline: false, activeAccountCount: 1, access: 'served', servedAccountId: 'y' });
  const selection = selectGettingStartedNode([accountsButNoApply, configuredButNoAccounts, servedButOffline]);
  assert.equal(selection.setupComplete, false);
  assert.deepEqual(selection.workingNodes, []);
  // Only a served account is access; an unavailable one is not.
  assert.equal(nodeSetupStage(node({ id: 'd', name: 'd', activeAccountCount: 1, pendingAccountId: 'z', pendingStatus: 'unavailable' })), 'access');
  assert.notEqual(nodeSetupStage(node({ id: 'e', name: 'e', activeAccountCount: 1, access: 'served', servedAccountId: null })), 'ready');
});

test('G: an MTProto-only node is ready when the Manager reports an account as served', () => {
  const settings = {
    serverId: 'm', protocol: 'mtproto',
    vless: { port: 0 }, reality: { enabled: false },
    wireGuard: { port: 0, address: '', dns: '', publicKey: '', ready: false },
    hysteria2: { port: 0, ready: false },
    shadowsocks: { port: 0, ready: false },
    mtproto: { port: 443, ready: true },
  } as unknown as ProtocolSettingsResponse;
  // No VLESS settings are required for an MTProto node.
  assert.equal(protocolConfigured(settings), true);
  const mtproto = node({
    id: 'm', name: 'mtproto', protocolConfigured: protocolConfigured(settings),
    activeAccountCount: 1, access: 'served', servedAccountId: 'acc-m',
  });
  assert.equal(nodeSetupStage(mtproto), 'ready');
});

test('unknown access is never reported as "no working VPN"', () => {
  const unknown = node({ id: 'u', name: 'us.routegate.org', activeAccountCount: 3, access: 'unknown' });
  assert.equal(nodeSetupStage(unknown), 'unknown');
  const alone = selectGettingStartedNode([unknown, ru]);
  assert.equal(alone.setupComplete, false);
  assert.deepEqual(alone.undeterminedNodes.map((item) => item.id), ['u']);
  // A working node still completes setup while another node is undetermined.
  const withWorking = selectGettingStartedNode([unknown, { ...us, id: 'w', name: 'de.routegate.org' }]);
  assert.equal(withWorking.setupComplete, true);
  assert.equal(withWorking.focus?.id, 'w');
});

test('I: the focus node is reported with its identity, never as the whole installation', () => {
  const selection = selectGettingStartedNode([ru, fi]);
  assert.equal(selection.setupComplete, false);
  assert.equal(selection.focus?.name, 'ru.routegate.org');
  assert.deepEqual(selection.otherNodes.map((item) => item.name), ['fi.routegate.org']);
  assert.deepEqual(selectGettingStartedNode([]), { setupComplete: false, workingNodes: [], focus: null, otherNodes: [], undeterminedNodes: [] });
});
