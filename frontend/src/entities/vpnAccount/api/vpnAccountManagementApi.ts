import { apiDelete, apiGet, apiPatch, apiPost } from '../../../shared/api/client';
import type { VpnAccount } from './vpnAccountApi';

export type VpnAccountStatus = 'created' | 'active' | 'suspended' | 'expired' | 'revoked';
export type BulkVpnAccountAction = 'activate' | 'suspend' | 'revoke' | 'delete' | 'assign_server';

export interface ManagedVpnAccount extends VpnAccount {
  configUpdatedAt?: string;
}

export interface VpnAccountListParams {
  search?: string;
  status?: string;
  serverId?: string;
  page?: number;
  pageSize?: number;
}

export interface PagedVpnAccountsResponse {
  items: ManagedVpnAccount[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface UpdateVpnAccountRequest {
  displayName?: string;
  email?: string;
  status?: VpnAccountStatus;
  expiresAt?: string;
  clearExpiresAt?: boolean;
  maxDevices?: number;
  clearMaxDevices?: boolean;
  serverId?: string;
}

export interface BulkVpnAccountSelection {
  ids?: string[];
  allMatching?: boolean;
  search?: string;
  status?: string;
  serverId?: string;
}

export interface BulkVpnAccountActionRequest {
  action: BulkVpnAccountAction;
  selection: BulkVpnAccountSelection;
  targetServerId?: string;
}

export interface BulkVpnAccountActionResponse {
  affectedCount: number;
  affectedServerIds: string[];
  configurationChanged: boolean;
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

export function getVpnAccount(vpnAccountId: string): Promise<ManagedVpnAccount> {
  return apiGet<ManagedVpnAccount>(`/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}`);
}

export function updateVpnAccount(
  vpnAccountId: string,
  request: UpdateVpnAccountRequest,
): Promise<ManagedVpnAccount> {
  return apiPatch<UpdateVpnAccountRequest, ManagedVpnAccount>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}`,
    request,
  );
}

export function deleteVpnAccount(vpnAccountId: string): Promise<void> {
  return apiDelete(`/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}`);
}

export function activateVpnAccountManagement(vpnAccountId: string): Promise<ManagedVpnAccount> {
  return apiPost<undefined, ManagedVpnAccount>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/activate`,
  );
}

export function suspendVpnAccount(vpnAccountId: string): Promise<ManagedVpnAccount> {
  return apiPost<undefined, ManagedVpnAccount>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/suspend`,
  );
}

export function revokeVpnAccount(vpnAccountId: string): Promise<ManagedVpnAccount> {
  return apiPost<undefined, ManagedVpnAccount>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/revoke`,
  );
}

export function runBulkVpnAccountAction(
  request: BulkVpnAccountActionRequest,
): Promise<BulkVpnAccountActionResponse> {
  const endpoint = request.action === 'activate' || request.action === 'assign_server'
    ? '/api/v1/vpn-accounts/bulk-update'
    : '/api/v1/vpn-accounts/bulk-disable';
  return apiPost<BulkVpnAccountActionRequest, BulkVpnAccountActionResponse>(endpoint, request);
}
