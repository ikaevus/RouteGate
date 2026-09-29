import assert from 'node:assert/strict';
import test from 'node:test';
import type {
  ClientProtocol,
  VpnClientConnectionStatus,
  VpnClientProfileStateResponse,
} from '../../entities/vpnAccount/api/vpnAccountApi.ts';
import {
  activationConfirmed,
  canApplyProtocolSet,
  protocolPreferenceView,
} from './protocolPreferenceModel.ts';

function state(
  connectionStatus: VpnClientConnectionStatus,
  enabledProtocols: ClientProtocol[] | undefined,
  activeProtocols: ClientProtocol[] | undefined,
  connectionMessage = '',
  activeProtocol: ClientProtocol = 'vless',
): VpnClientProfileStateResponse {
  return {
    vpnAccountId: 'account-id',
    activeProtocol,
    connectionStatus,
    connectionMessage,
    profile: {
      id: 'profile-id', vpnAccountId: 'account-id', name: '', clientType: 'generic', deviceType: 'other',
      fingerprintMode: 'auto', fingerprint: '', resolvedFingerprint: 'chrome', spiderX: '/', protocol: 'auto',
      createdAt: '2026-09-29T00:00:00Z', updatedAt: '2026-09-29T00:00:00Z',
      enabledProtocols, activeProtocols,
    },
  };
}

const awaitingMessage = "the node has not received this account's vless access yet; render and successfully apply a new configuration for the node, then retry";

test('an account the node has not been given stays editable with the apply action available', () => {
  const view = protocolPreferenceView(state('awaiting_apply', ['vless'], [], awaitingMessage));
  assert.deepEqual(view.desired, ['vless']);
  assert.deepEqual(view.active, []);
  assert.equal(view.awaitingDeployment, true);
  assert.equal(view.activationPending, true);
  assert.match(view.message, /render and successfully apply/);
  assert.equal(canApplyProtocolSet(view, false), true, 'apply must stay available without local edits');
});

test('a node without any applied configuration is awaiting its first apply', () => {
  const view = protocolPreferenceView(state('awaiting_first_apply', ['vless'], []));
  assert.equal(view.awaitingDeployment, true);
  assert.deepEqual(view.active, []);
  assert.equal(canApplyProtocolSet(view, false), true);
});

test('a new protocol for a served account is pending while working access stays active', () => {
  const view = protocolPreferenceView(state('ready', ['vless', 'shadowsocks'], ['vless']));
  assert.equal(view.awaitingDeployment, false);
  assert.equal(view.activationPending, true);
  assert.deepEqual(view.active, ['vless']);
  assert.deepEqual(view.desired, ['vless', 'shadowsocks']);
  assert.equal(canApplyProtocolSet(view, false), true);
});

test('a served account in sync needs no apply until something changes', () => {
  const view = protocolPreferenceView(state('ready', ['vless', 'shadowsocks'], ['shadowsocks', 'vless']));
  assert.equal(view.activationPending, false);
  assert.equal(canApplyProtocolSet(view, false), false);
  assert.equal(canApplyProtocolSet(view, true), true);
});

test('the panel returns to normal only after the apply really serves the saved set', () => {
  const enabled: ClientProtocol[] = ['vless', 'shadowsocks'];
  assert.equal(activationConfirmed(state('awaiting_apply', enabled, []), enabled), false);
  assert.equal(activationConfirmed(state('ready', enabled, ['vless']), enabled), false);
  assert.equal(activationConfirmed(state('ready', enabled, ['vless', 'shadowsocks']), enabled), true);
});

test('legacy responses without protocol sets fall back to the primary protocol only when served', () => {
  assert.deepEqual(protocolPreferenceView(state('ready', undefined, undefined)).active, ['vless']);
  assert.deepEqual(protocolPreferenceView(state('awaiting_apply', undefined, undefined)).active, []);
  assert.deepEqual(protocolPreferenceView(state('awaiting_apply', undefined, undefined)).desired, ['vless']);
});

test('MTProto served by the applied node-wide proxy counts as active without a saved protocol row', () => {
  const view = protocolPreferenceView(state('ready', [], ['mtproto'], '', 'mtproto'));
  assert.deepEqual(view.active, ['mtproto']);
  assert.deepEqual(view.desired, ['mtproto']);
  assert.equal(view.activationPending, false);
  assert.equal(canApplyProtocolSet(view, false), false);
  assert.equal(activationConfirmed(state('ready', [], ['mtproto'], '', 'mtproto'), ['mtproto']), true);
});

test('MTProto is not reported active once the applied version no longer runs the proxy', () => {
  const view = protocolPreferenceView(state('awaiting_apply', ['mtproto'], [], awaitingMessage, 'mtproto'));
  assert.deepEqual(view.active, []);
  assert.equal(view.awaitingDeployment, true);
  assert.equal(canApplyProtocolSet(view, false), true);
});
