import { apiGet, apiPatch, apiPost } from '../../../shared/api/client';

export interface VpnAccount {
  id: string;
  displayName: string;
  email?: string | null;
  status: string;
  expiresAt?: string | null;
  maxDevices?: number | null;
  serverId?: string | null;
  vlessUuid?: string | null;
  createdAt: string;
  updatedAt: string;
  configUpdatedAt?: string;
}

export interface ListVpnAccountsResponse {
  items: VpnAccount[];
}

export interface CreateVpnAccountRequest {
  displayName: string;
  email?: string;
  serverId?: string;
}

export interface VpnAccountCredentialsResponse {
  vpnAccountId: string;
  serverId?: string;
  endpoint?: string;
  protocol: string;
  vless: {
    uuid: string;
    flow?: string;
    network?: string;
  };
  reality: {
    enabled: boolean;
    publicKey?: string;
    shortId?: string;
    serverName?: string;
  };
  wireGuard: {
    privateKey?: string;
    publicKey?: string;
    address?: string;
    serverPublicKey?: string;
    dns?: string;
  };
}

export interface TrafficUsageSummaryResponse {
  vpnAccountId: string;
  period: {
    from: string;
    to: string;
  };
  usage: {
    rxBytes: number;
    txBytes: number;
    totalBytes: number;
  };
  limit?: {
    monthlyLimitBytes?: number | null;
    hardLimitEnabled: boolean;
    speedLimitBps?: number | null;
    resetDay: number;
    usedPercent?: number | null;
    remainingBytes?: number | null;
    limitReached: boolean;
    enforced: boolean;
    limitExceededAt?: string | null;
    enforcementStatus: string;
    enforcementUpdatedAt?: string | null;
    updatedAt: string;
  } | null;
}

export interface UpdateTrafficLimitRequest {
  monthlyLimitBytes?: number | null;
  hardLimitEnabled: boolean;
  speedLimitBps?: number | null;
  resetDay?: number | null;
}

export interface TrafficLimitResponse {
  vpnAccountId: string;
  monthlyLimitBytes?: number | null;
  hardLimitEnabled: boolean;
  speedLimitBps?: number | null;
  resetDay: number;
  limitExceededAt?: string | null;
  enforcementStatus: string;
  enforcementUpdatedAt?: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface SubscriptionTokenResponse {
  vpnAccountId: string;
  subscriptionToken: string;
  subscriptionUrl: string;
  expiresAt?: string | null;
}

export interface SubscriptionQRCodeResponse {
  vpnAccountId: string;
  subscriptionUrl: string;
  qrText: string;
  format: string;
}

export type ClientFingerprintMode = 'auto' | 'manual';

export interface VpnClientProfile {
  id: string;
  vpnAccountId: string;
  name: string;
  clientType: string;
  deviceType: string;
  fingerprintMode: ClientFingerprintMode;
  fingerprint: string;
  resolvedFingerprint: string;
  serverNameOverride?: string;
  spiderX: string;
  mtu?: number | null;
  createdAt: string;
  updatedAt: string;
}

export interface VpnClientConnectionResponse {
  vpnAccountId: string;
  format: string;
  vlessLink?: string;
  wireGuardConfig?: string;
  profile: VpnClientProfile;
  endpoint: string;
  serverName: string;
  network: string;
  flow?: string;
}

export interface UpdateVpnClientProfileRequest {
  name: string;
  clientType: string;
  deviceType: string;
  fingerprintMode: ClientFingerprintMode;
  fingerprint: string;
  serverNameOverride: string;
  spiderX: string;
  mtu?: number | null;
}

interface SingBoxClientReality {
  enabled?: boolean;
  public_key?: string;
  short_id?: string;
}

interface SingBoxClientTLS {
  enabled?: boolean;
  server_name?: string;
  reality?: SingBoxClientReality;
}

interface SingBoxClientOutbound {
  type?: string;
  server?: string;
  server_port?: number;
  uuid?: string;
  flow?: string;
  network?: string;
  tls?: SingBoxClientTLS;
}

interface SingBoxClientConfig {
  outbounds?: SingBoxClientOutbound[];
  [key: string]: unknown;
}

export interface PublicSubscriptionResponse {
  status: string;
  format: string;
  generatedAt: string;
  vpnAccountId: string;
  account: {
    id: string;
    displayName: string;
    status: string;
    expiresAt?: string | null;
    maxDevices?: number | null;
  };
  server?: {
    id: string;
    name: string;
    hostname?: string;
    publicIp?: string;
    endpoint?: string;
    location?: string;
    provider?: string;
  } | null;
  config: {
    type: string;
    format: string;
    status: string;
    message?: string;
    rendered?: {
      format: string;
		content: SingBoxClientConfig;
		text?: string;
    } | null;
  };
}

function requiredText(value: string | undefined, field: string): string {
  const normalized = value?.trim() ?? '';
  if (normalized === '') {
    throw new Error(`${field} is required to create a VLESS connection link.`);
  }
  return normalized;
}

function formatVlessHost(server: string): string {
  if (server.includes(':') && !server.startsWith('[')) {
    return `[${server}]`;
  }
  return server;
}

export function buildVlessRealityShareLink(
  subscription: PublicSubscriptionResponse,
  fingerprint = 'firefox',
): string {
	const content = subscription.config.rendered?.content;
  const outbound = content?.outbounds?.find(
    (candidate) => candidate.type?.trim().toLowerCase() === 'vless',
  );
  if (!outbound) {
    throw new Error('Rendered client config does not contain a VLESS outbound.');
  }

  const server = requiredText(outbound.server, 'VLESS server');
  const uuid = requiredText(outbound.uuid, 'VLESS UUID');
  const serverName = requiredText(outbound.tls?.server_name, 'Reality server name');
  const publicKey = requiredText(outbound.tls?.reality?.public_key, 'Reality public key');
  const serverPort = outbound.server_port;
  if (typeof serverPort !== 'number' || !Number.isInteger(serverPort) || serverPort < 1 || serverPort > 65535) {
    throw new Error('VLESS server port is invalid.');
  }
  if (!outbound.tls?.enabled || !outbound.tls.reality?.enabled) {
    throw new Error('Rendered client config does not enable Reality.');
  }

  const parameters = new URLSearchParams();
  parameters.set('encryption', 'none');
  parameters.set('security', 'reality');
  parameters.set('type', outbound.network?.trim() || 'tcp');
  parameters.set('sni', serverName);
  parameters.set('fp', fingerprint);
  parameters.set('pbk', publicKey);
  if (typeof outbound.tls.reality.short_id === 'string') {
    parameters.set('sid', outbound.tls.reality.short_id.trim());
  }
  if (outbound.flow?.trim()) {
    parameters.set('flow', outbound.flow.trim());
  }

  const label = subscription.account.displayName.trim()
    || subscription.server?.name?.trim()
    || 'RouteGate';

  return `vless://${encodeURIComponent(uuid)}@${formatVlessHost(server)}:${serverPort}?${parameters.toString()}#${encodeURIComponent(label)}`;
}

export async function getVpnAccounts(): Promise<ListVpnAccountsResponse> {
  const servers = await apiGet<{ items: Array<{ id: string }> }>('/api/v1/servers');
  if (servers.items.length === 0) return { items: [] };

  const results = await Promise.all(servers.items.map((server) => apiGet<{ items: VpnAccount[] }>(
    `/api/v1/vpn-accounts?status=active&serverId=${encodeURIComponent(server.id)}&page=1&pageSize=1`,
  )));

  return {
    items: results.flatMap((result) => result.items).map((account) => ({
      ...account,
      // Getting Started historically reads updatedAt for config freshness.
      // Preserve that contract while separating generic metadata edits in the API.
      updatedAt: account.configUpdatedAt ?? account.updatedAt,
    })),
  };
}

export async function createVpnAccount(request: CreateVpnAccountRequest): Promise<VpnAccount> {
  const account = await apiPost<CreateVpnAccountRequest, VpnAccount>('/api/v1/vpn-accounts', request);
  return activateVpnAccount(account.id);
}

export function activateVpnAccount(vpnAccountId: string): Promise<VpnAccount> {
  return apiPost<undefined, VpnAccount>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/activate`,
  );
}

export function getVpnAccountCredentials(
  vpnAccountId: string,
): Promise<VpnAccountCredentialsResponse> {
  return apiGet<VpnAccountCredentialsResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/credentials`,
  );
}

export function getVpnAccountClientConnection(
  vpnAccountId: string,
): Promise<VpnClientConnectionResponse> {
  return apiGet<VpnClientConnectionResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/client-connection`,
  );
}

export function updateVpnAccountClientProfile(
  vpnAccountId: string,
  request: UpdateVpnClientProfileRequest,
): Promise<VpnClientConnectionResponse> {
  return apiPatch<UpdateVpnClientProfileRequest, VpnClientConnectionResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/client-profile`,
    request,
  );
}

export function getVpnAccountTraffic(
  vpnAccountId: string,
): Promise<TrafficUsageSummaryResponse> {
  return apiGet<TrafficUsageSummaryResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/traffic`,
  );
}

export function updateVpnAccountTrafficLimit(
  vpnAccountId: string,
  request: UpdateTrafficLimitRequest,
): Promise<TrafficLimitResponse> {
  return apiPatch<UpdateTrafficLimitRequest, TrafficLimitResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/traffic-limit`,
    request,
  );
}

export function createVpnAccountSubscriptionToken(
  vpnAccountId: string,
): Promise<SubscriptionTokenResponse> {
  return apiPost<undefined, SubscriptionTokenResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/subscription-token`,
  );
}

export function rotateVpnAccountSubscriptionToken(
  vpnAccountId: string,
): Promise<SubscriptionTokenResponse> {
  return apiPost<undefined, SubscriptionTokenResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/subscription-token/rotate`,
  );
}

export async function getVpnAccountSubscriptionQRCode(
  vpnAccountId: string,
  subscriptionToken: string,
): Promise<SubscriptionQRCodeResponse> {
  const subscription = await getPublicSubscription(subscriptionToken);
  if (subscription.vpnAccountId !== vpnAccountId) {
    throw new Error('Subscription belongs to a different VPN account.');
  }

	const renderedText = subscription.config.rendered?.text;
	const isWireGuard = subscription.config.type === 'wireguard' && typeof renderedText === 'string';
  return {
    vpnAccountId,
    subscriptionUrl: new URL(
      `/api/v1/subscriptions/${encodeURIComponent(subscriptionToken)}`,
      globalThis.location.origin,
    ).toString(),
		qrText: isWireGuard ? renderedText : buildVlessRealityShareLink(subscription),
		format: isWireGuard ? 'wireguard-config' : 'vless-reality-uri',
  };
}

export function getPublicSubscription(
  subscriptionToken: string,
): Promise<PublicSubscriptionResponse> {
  return apiGet<PublicSubscriptionResponse>(
    `/api/v1/subscriptions/${encodeURIComponent(subscriptionToken)}`,
  );
}
