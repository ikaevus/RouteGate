import { apiGet, apiPatch, apiPost } from '../../../shared/api/client';
import type { ClientCompatibilityAssessment } from '../model/clientCompatibility';

export type DeviceClientType = 'hiddify' | 'v2rayn' | 'v2rayng' | 'generic';
export type DevicePlatform = 'windows' | 'ios' | 'android' | 'macos' | 'linux' | 'other';
export type DeviceStatus = 'active' | 'revoked';

export interface VpnAccountDevice {
  id: string;
  vpnAccountId: string;
  name: string;
  clientType: DeviceClientType;
  deviceType: DevicePlatform;
  status: DeviceStatus;
  createdAt: string;
  updatedAt: string;
  lastUsedAt?: string | null;
  revokedAt?: string | null;
}

export interface VpnAccountDeviceAccess {
  device: VpnAccountDevice;
  subscriptionUrl?: string;
  tokenPreview?: string;
  tokenExpiresAt?: string | null;
  tokenLastUsedAt?: string | null;
  hasActiveToken: boolean;
  compatibility: ClientCompatibilityAssessment;
}

export interface CreateVpnAccountDeviceRequest {
  name: string;
  clientType: DeviceClientType;
  deviceType: DevicePlatform;
}

export interface UpdateVpnAccountDeviceRequest {
  name?: string;
  clientType?: DeviceClientType;
  deviceType?: DevicePlatform;
}

export interface VpnAccountDeviceTokenResponse {
  device: VpnAccountDevice;
  subscriptionToken: string;
  tokenPreview: string;
  subscriptionUrl: string;
  expiresAt?: string | null;
}

export function listVpnAccountDevices(vpnAccountId: string): Promise<{ items: VpnAccountDeviceAccess[] }> {
  return apiGet<{ items: VpnAccountDeviceAccess[] }>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/devices`,
  );
}

export function createVpnAccountDevice(
  vpnAccountId: string,
  request: CreateVpnAccountDeviceRequest,
): Promise<VpnAccountDeviceTokenResponse> {
  return apiPost<CreateVpnAccountDeviceRequest, VpnAccountDeviceTokenResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/devices`,
    request,
  );
}

export function updateVpnAccountDevice(
  vpnAccountId: string,
  deviceId: string,
  request: UpdateVpnAccountDeviceRequest,
): Promise<VpnAccountDeviceAccess> {
  return apiPatch<UpdateVpnAccountDeviceRequest, VpnAccountDeviceAccess>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/devices/${encodeURIComponent(deviceId)}`,
    request,
  );
}

export function rotateVpnAccountDeviceToken(
  vpnAccountId: string,
  deviceId: string,
): Promise<VpnAccountDeviceTokenResponse> {
  return apiPost<undefined, VpnAccountDeviceTokenResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/devices/${encodeURIComponent(deviceId)}/rotate`,
  );
}

export function revokeVpnAccountDevice(
  vpnAccountId: string,
  deviceId: string,
): Promise<VpnAccountDeviceAccess> {
  return apiPost<undefined, VpnAccountDeviceAccess>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/devices/${encodeURIComponent(deviceId)}/revoke`,
  );
}
