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
      content: Record<string, unknown>;
    } | null;
  };
}

export function getVpnAccounts(): Promise<ListVpnAccountsResponse> {
  return apiGet<ListVpnAccountsResponse>('/api/v1/vpn-accounts');
}

export function createVpnAccount(request: CreateVpnAccountRequest): Promise<VpnAccount> {
  return apiPost<CreateVpnAccountRequest, VpnAccount>('/api/v1/vpn-accounts', request);
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

export function getVpnAccountSubscriptionQRCode(
  vpnAccountId: string,
  subscriptionToken: string,
): Promise<SubscriptionQRCodeResponse> {
  const params = new URLSearchParams({ token: subscriptionToken });

  return apiGet<SubscriptionQRCodeResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/qr?${params.toString()}`,
  );
}

export function getPublicSubscription(
  subscriptionToken: string,
): Promise<PublicSubscriptionResponse> {
  return apiGet<PublicSubscriptionResponse>(
    `/api/v1/subscriptions/${encodeURIComponent(subscriptionToken)}`,
  );
}
