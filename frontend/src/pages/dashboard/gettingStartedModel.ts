import type { ProtocolSettingsResponse } from '../../entities/server/api/serverApi.ts';
import type { VpnClientConnectionStatus } from '../../entities/vpnAccount/api/vpnAccountApi.ts';

/**
 * Getting Started state, evaluated per node.
 *
 * Every fact of a node's setup (Agent, runtime, protocol settings, applied
 * configuration, account access) belongs to that node only: readiness of
 * different nodes is never combined. A node provides VPN access when its Agent
 * is online, it has an Agent-confirmed applied configuration and at least one
 * of its active accounts is served by that configuration, as reported by the
 * Manager's client-profile state (the same evaluation that issues client links,
 * including the node-wide MTProto proxy). Saved settings or an active account
 * alone never count as a working VPN.
 *
 * The first-run setup of the installation is complete as soon as one node
 * provides access; an extra empty node does not reopen it.
 */

/** Stage of one node; 'ready' means the node serves at least one active account. */
export type NodeSetupStage = 'connect' | 'core' | 'protocol' | 'account' | 'apply' | 'access' | 'ready';

export interface NodeSetupFacts {
  id: string;
  name: string;
  agentOnline: boolean;
  coreInstalled: boolean;
  /** Saved protocol settings are complete (not proof that they are applied). */
  protocolConfigured: boolean;
  /** The Manager has an Agent-confirmed applied configuration for the node. */
  appliedVersion: boolean;
  /** Active accounts assigned to the node. */
  activeAccountCount: number;
  /** First active account (oldest first) the applied configuration serves. */
  readyAccountId: string | null;
  /** Access state of the oldest checked account when none is ready. */
  accountStatus: VpnClientConnectionStatus | null;
  accountMessage?: string | null;
  /** The account whose state is reported (the oldest checked account). */
  checkedAccountId: string | null;
  /** The most recent apply job of the node failed. */
  latestApplyFailed: boolean;
  latestApplyError?: string | null;
}

/** Steps shown by the wizard, all evaluated for one node. */
export const setupStepKeys = ['installed', 'server', 'core', 'protocol', 'final'] as const;
export type SetupStepKey = (typeof setupStepKeys)[number];

const stageCompletedSteps: Record<NodeSetupStage, number> = {
  connect: 1,
  core: 2,
  protocol: 3,
  account: 4,
  apply: 4,
  access: 4,
  ready: 5,
};

export function nodeSetupStage(node: NodeSetupFacts): NodeSetupStage {
  if (!node.agentOnline) return 'connect';
  // The applied configuration is the durable proof of a working node: it stays
  // valid when settings were changed or saved again after the last apply.
  if (node.appliedVersion && node.readyAccountId) return 'ready';
  if (!node.coreInstalled) return 'core';
  if (!node.protocolConfigured) return 'protocol';
  if (node.activeAccountCount === 0) return 'account';
  if (!node.appliedVersion) return 'apply';
  if (node.accountStatus === 'awaiting_apply' || node.accountStatus === 'awaiting_first_apply') return 'apply';
  return 'access';
}

/** Number of completed wizard steps (out of 5) for a node, with the Manager ready. */
export function nodeCompletedSteps(node: NodeSetupFacts): number {
  return stageCompletedSteps[nodeSetupStage(node)];
}

function compareNodes(left: NodeSetupFacts, right: NodeSetupFacts): number {
  const byName = left.name.localeCompare(right.name, 'en', { sensitivity: 'base' });
  if (byName !== 0) return byName;
  return left.id < right.id ? -1 : left.id > right.id ? 1 : 0;
}

export interface GettingStartedSelection {
  /** True when at least one node provides VPN access. */
  setupComplete: boolean;
  /** Nodes that provide VPN access, in a stable order. */
  workingNodes: NodeSetupFacts[];
  /**
   * The node the wizard describes: the first working node when setup is
   * complete, otherwise the node closest to providing access. Null when there
   * is no VPN node at all.
   */
  focus: NodeSetupFacts | null;
  /** Other VPN nodes that do not serve any account yet (stable order). */
  otherNodes: NodeSetupFacts[];
}

/**
 * Selects the node the wizard describes. The result never depends on the order
 * in which the API lists nodes: ties are broken by name, then id.
 */
export function selectGettingStartedNode(nodes: NodeSetupFacts[]): GettingStartedSelection {
  const sorted = [...nodes].sort(compareNodes);
  const workingNodes = sorted.filter((node) => nodeSetupStage(node) === 'ready');
  if (workingNodes.length > 0) {
    return {
      setupComplete: true,
      workingNodes,
      focus: workingNodes[0],
      otherNodes: sorted.filter((node) => nodeSetupStage(node) !== 'ready'),
    };
  }
  let focus: NodeSetupFacts | null = null;
  for (const node of sorted) {
    if (!focus || nodeCompletedSteps(node) > nodeCompletedSteps(focus)) focus = node;
  }
  return {
    setupComplete: false,
    workingNodes: [],
    focus,
    otherNodes: sorted.filter((node) => node !== focus),
  };
}

function textPresent(value?: string | null): boolean {
  return typeof value === 'string' && value.trim() !== '';
}

function validPort(port: number): boolean {
  return port >= 1 && port <= 65535;
}

/** Saved protocol settings of a node are complete for its selected protocol. */
export function protocolConfigured(settings?: ProtocolSettingsResponse): boolean {
  if (!settings) return false;
  const protocol = settings.protocol.trim().toLowerCase();
  if (protocol === 'wireguard') {
    return settings.wireGuard.ready
      && validPort(settings.wireGuard.port)
      && textPresent(settings.wireGuard.address)
      && textPresent(settings.wireGuard.publicKey);
  }
  if (protocol === 'hysteria2') return settings.hysteria2.ready && validPort(settings.hysteria2.port);
  if (protocol === 'shadowsocks') return settings.shadowsocks.ready && validPort(settings.shadowsocks.port);
  if (protocol === 'mtproto') return settings.mtproto.ready && validPort(settings.mtproto.port);
  return protocol === 'vless'
    && validPort(settings.vless.port)
    && settings.reality.enabled
    && textPresent(settings.reality.publicKey)
    && textPresent(settings.reality.shortId)
    && textPresent(settings.reality.serverName);
}

/** Accounts whose access is checked per node: oldest first, at most this many. */
export const accountsCheckedPerNode = 3;

export interface AccountForSetup {
  id: string;
  serverId?: string | null;
  status: string;
  createdAt: string;
}

/** Active accounts of a node, oldest first (then by id), for a stable check order. */
export function activeAccountsOfNode(accounts: AccountForSetup[], nodeId: string): AccountForSetup[] {
  return accounts
    .filter((account) => account.serverId === nodeId && account.status.trim().toLowerCase() === 'active')
    .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || (left.id < right.id ? -1 : left.id > right.id ? 1 : 0));
}
