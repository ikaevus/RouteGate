import { apiGet } from '../../../shared/api/client';

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

export function getVpnAccounts(): Promise<ListVpnAccountsResponse> {
  return apiGet<ListVpnAccountsResponse>('/api/v1/vpn-accounts');
}

export function getVpnAccountCredentials(
  vpnAccountId: string,
): Promise<VpnAccountCredentialsResponse> {
  return apiGet<VpnAccountCredentialsResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/credentials`,
  );
}
