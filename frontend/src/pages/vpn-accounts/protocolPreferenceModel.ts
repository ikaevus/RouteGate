import type {
  ClientProtocol,
  VpnClientConnectionStatus,
  VpnClientProfileStateResponse,
} from '../../entities/vpnAccount/api/vpnAccountApi';

export const protocolOrder: ClientProtocol[] = ['vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'];

export function ordered(values: readonly ClientProtocol[]): ClientProtocol[] {
  const selected = new Set(values);
  return protocolOrder.filter((protocol) => selected.has(protocol));
}

export function sameProtocols(left: readonly ClientProtocol[], right: readonly ClientProtocol[]): boolean {
  const a = ordered(left);
  const b = ordered(right);
  return a.length === b.length && a.every((value, index) => value === b[index]);
}

export interface ProtocolPreferenceView {
  /** Saved desired set. */
  desired: ClientProtocol[];
  /** Protocols the node actually serves for the account now. */
  active: ClientProtocol[];
  status: VpnClientConnectionStatus;
  message: string;
  /** The node has not been given this account's access yet. */
  awaitingDeployment: boolean;
  /** The saved desired set differs from what the node serves. */
  activationPending: boolean;
}

/**
 * Derives what the protocol panel shows from the client profile state. The
 * state never carries client links, so it is available while access is
 * withheld and the administrator can still edit and apply the set.
 */
export function protocolPreferenceView(state: VpnClientProfileStateResponse): ProtocolPreferenceView {
  const status = state.connectionStatus;
  const served = state.profile.activeProtocols ?? [];
  const active = ordered(served.length ? served : status === 'ready' ? [state.activeProtocol] : []);
  const saved = state.profile.enabledProtocols ?? [];
  const desired = ordered(saved.length ? saved : active.length ? active : [state.activeProtocol]);
  const awaitingDeployment = status === 'awaiting_apply' || status === 'awaiting_first_apply';
  return {
    desired,
    active,
    status,
    message: state.connectionMessage?.trim() ?? '',
    awaitingDeployment,
    activationPending: awaitingDeployment || !sameProtocols(desired, active),
  };
}

/**
 * The apply action stays available whenever the saved set is not what the
 * node serves, including an account the node has not been given yet.
 */
export function canApplyProtocolSet(view: ProtocolPreferenceView, changed: boolean): boolean {
  return changed || view.activationPending;
}

/** The panel returns to the normal state only once access is really served. */
export function activationConfirmed(state: VpnClientProfileStateResponse, enabled: readonly ClientProtocol[]): boolean {
  const view = protocolPreferenceView(state);
  return view.status === 'ready' && sameProtocols(view.active, enabled);
}
