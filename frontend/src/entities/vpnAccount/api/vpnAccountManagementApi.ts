import { apiDelete, apiGet, apiPatch, apiPost } from '../../../shared/api/client';
import type { VpnAccount } from './vpnAccountApi';

export type VpnAccountStatus = 'created' | 'active' | 'suspended' | 'expired' | 'revoked';

export interface VpnAccountListParams {
  search?: string;
  status?: string;
  serverId?: string;
  page?: number;
  pageSize?: number;
}

export interface PagedVpnAccountsResponse {
  items: VpnAccount[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface UpdateVpnAccountRequest {
  displayName?: string;
  email?: string;
  status?: VpnAccountStatus;
  expiresAt?: string | null;
  maxDevices?: number | null;
  serverId?: string;
}

function listQuery(params: VpnAccountListParams): string {
  const query = new URLSearchParams();
  if (params.search?.trim()) query.set('search', params.search.trim());
  if (params.status?.trim()) query.set('status', params.status.trim());
  if (params.serverId?.trim()) query.set('serverId', params.serverId.trim());
  query.set('page', String(params.page ?? 1));
  query.set('pageSize', String(params.pageSize ?? 50));
  return query.toString();
}

export function getPagedVpnAccounts(params: VpnAccountListParams): Promise<PagedVpnAccountsResponse> {
  return apiGet<PagedVpnAccountsResponse>(`/api/v1/vpn-accounts?${listQuery(params)}`);
}

export function getVpnAccount(vpnAccountId: string): Promise<VpnAccount> {
  return apiGet<VpnAccount>(`/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}`);
}

export function updateVpnAccount(
  vpnAccountId: string,
  request: UpdateVpnAccountRequest,
): Promise<VpnAccount> {
  return apiPatch<UpdateVpnAccountRequest, VpnAccount>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}`,
    request,
  );
}

export function deleteVpnAccount(vpnAccountId: string): Promise<void> {
  return apiDelete(`/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}`);
}

export function activateVpnAccountManagement(vpnAccountId: string): Promise<VpnAccount> {
  return apiPost<undefined, VpnAccount>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/activate`,
  );
}

export function suspendVpnAccount(vpnAccountId: string): Promise<VpnAccount> {
  return apiPost<undefined, VpnAccount>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/suspend`,
  );
}

export function revokeVpnAccount(vpnAccountId: string): Promise<VpnAccount> {
  return apiPost<undefined, VpnAccount>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/revoke`,
  );
}
