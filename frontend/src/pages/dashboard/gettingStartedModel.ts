import type { ProtocolSettingsResponse } from '../../entities/server/api/serverApi.ts';
import type { VpnClientConnectionStatus } from '../../entities/vpnAccount/api/vpnAccountApi.ts';

/**
 * Getting Started state, evaluated per node.
 *
 * Every fact of a node's setup (Agent, runtime, protocol settings, account
 * access) belongs to that node only: readiness of different nodes is never
 * combined. A node provides VPN access when its Agent is online and the
 * Manager's access summary reports that at least one of its active accounts,
 * of any age, gets client access: the evaluation that issues client links
 * from the applied configuration (protocol sets, node-wide MTProto proxy).
 * Saved settings, an applied version or an active account alone never count
 * as a working VPN.
 *
 * The first-run setup of the installation is complete as soon as one node
 * provides access; an extra empty node does not reopen it.
 */

/**
 * Stage of one node; 'ready' means the node serves at least one active
 * account, 'unknown' that the Manager could not finish evaluating it.
 */
export type NodeSetupStage = 'connect' | 'core' | 'protocol' | 'account' | 'apply' | 'access' | 'unknown' | 'ready';

/** Client access of a node as the Manager's access summary reports it. */
export type NodeAccessState = 'served' | 'not_served' | 'unknown';

export interface NodeSetupFacts {
  id: string;
  name: string;
  agentOnline: boolean;
  coreInstalled: boolean;
  /** Saved protocol settings are complete (not proof that they are applied). */
  protocolConfigured: boolean;
  /** Active accounts assigned to the node. */
  activeAccountCount: number;
  /** Whether the node issues client access to at least one active account. */
  access: NodeAccessState;
  /** An active account the node serves (any age), when access is 'served'. */
  servedAccountId: string | null;
  /** First evaluated account without access, with its connection status. */
  pendingAccountId: string | null;
  pendingStatus: VpnClientConnectionStatus | null;
  pendingMessage?: string | null;
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
  unknown: 4,
  ready: 5,
};

export function nodeSetupStage(node: NodeSetupFacts): NodeSetupStage {
  if (!node.agentOnline) return 'connect';
  // Access issued from the applied configuration is the durable proof of a
  // working node: it stays valid when settings were saved again after the
  // last apply or a later apply failed.
  if (node.access === 'served' && node.servedAccountId) return 'ready';
  // An unfinished evaluation is not evidence that the node serves nobody.
  if (node.access === 'unknown' && node.activeAccountCount > 0) return 'unknown';
  if (!node.coreInstalled) return 'core';
  if (!node.protocolConfigured) return 'protocol';
  if (node.activeAccountCount === 0) return 'account';
  if (node.pendingStatus === 'awaiting_apply' || node.pendingStatus === 'awaiting_first_apply') return 'apply';
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
  /**
   * Nodes whose access could not be evaluated. Without a working node they
   * leave the setup state undetermined: never reported as "no working VPN".
   */
  undeterminedNodes: NodeSetupFacts[];
}

/**
 * Selects the node the wizard describes. The result never depends on the order
 * in which the API lists nodes: ties are broken by name, then id.
 */
export function selectGettingStartedNode(nodes: NodeSetupFacts[]): GettingStartedSelection {
  const sorted = [...nodes].sort(compareNodes);
  const workingNodes = sorted.filter((node) => nodeSetupStage(node) === 'ready');
  const undeterminedNodes = sorted.filter((node) => nodeSetupStage(node) === 'unknown');
  if (workingNodes.length > 0) {
    return {
      setupComplete: true,
      workingNodes,
      focus: workingNodes[0],
      otherNodes: sorted.filter((node) => nodeSetupStage(node) !== 'ready'),
      undeterminedNodes,
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
    undeterminedNodes,
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
