import { apiGet, apiPost } from '../../../shared/api/client';

export interface PortalUser {
  id: string;
  email: string;
  username?: string;
  displayName?: string;
  status: string;
}

export interface PortalProfile {
  id: string;
  displayName: string;
  status: string;
  accessStatus: string;
  expiresAt?: string | null;
  maxDevices?: number | null;
  protocol: string;
  location?: string;
  updatedAt: string;
}

export interface PortalNotice {
  type: string;
  message: string;
}

export interface PortalTrafficUsage {
  enabled: boolean;
  rxBytes: number;
  txBytes: number;
  totalBytes: number;
  periodStart: string;
  periodEnd: string;
  lastObservedAt?: string | null;
}

export interface PortalDashboard {
  accessStatus: string;
  profilesTotal: number;
  profilesActive: number;
  nearestExpiration?: string | null;
  trafficUsage?: PortalTrafficUsage | null;
  notices: PortalNotice[];
}

export interface PortalSubscription {
  profileId: string;
  available: boolean;
  accessStatus: string;
  subscriptionUrl?: string;
  format: string;
  expiresAt?: string | null;
  requiresTokenRotation: boolean;
  message?: string;
}

export interface PortalQRCode {
  profileId: string;
  available: boolean;
  accessStatus: string;
  qrText?: string;
  format: string;
  message?: string;
}

export interface InstructionPlatform {
  platform: string;
  displayName: string;
  description: string;
}

export interface DeviceInstruction {
  platform: string;
  displayName: string;
  steps: string[];
  notes: string[];
}

export interface PortalMeResponse {
  user: PortalUser;
}

export interface PortalDashboardResponse {
  dashboard: PortalDashboard;
}

export interface PortalProfilesResponse {
  items: PortalProfile[];
}

export interface PortalProfileResponse {
  profile: PortalProfile;
}

export interface PortalSubscriptionResponse {
  subscription: PortalSubscription;
}

export interface PortalQRCodeResponse {
  qr: PortalQRCode;
}

export interface PortalSubscriptionAccessResponse {
  subscription: PortalSubscription;
  qr: PortalQRCode;
}

export interface PortalInstructionsResponse {
  items: InstructionPlatform[];
}

export interface PortalInstructionResponse {
  instruction: DeviceInstruction;
}

export function getPortalMe(): Promise<PortalMeResponse> {
  return apiGet<PortalMeResponse>('/api/portal/me');
}

export function getPortalDashboard(): Promise<PortalDashboardResponse> {
  return apiGet<PortalDashboardResponse>('/api/portal/dashboard');
}

export function getPortalProfiles(): Promise<PortalProfilesResponse> {
  return apiGet<PortalProfilesResponse>('/api/portal/profiles');
}

export function getPortalProfile(profileId: string): Promise<PortalProfileResponse> {
  return apiGet<PortalProfileResponse>(`/api/portal/profiles/${encodeURIComponent(profileId)}`);
}

export function getPortalSubscription(profileId: string): Promise<PortalSubscriptionResponse> {
  return apiGet<PortalSubscriptionResponse>(
    `/api/portal/profiles/${encodeURIComponent(profileId)}/subscription`,
  );
}

export function generatePortalSubscriptionAccess(
  profileId: string,
): Promise<PortalSubscriptionAccessResponse> {
  return apiPost<undefined, PortalSubscriptionAccessResponse>(
    `/api/portal/profiles/${encodeURIComponent(profileId)}/subscription-token`,
  );
}

export function getPortalQRCode(profileId: string): Promise<PortalQRCodeResponse> {
  return apiGet<PortalQRCodeResponse>(`/api/portal/profiles/${encodeURIComponent(profileId)}/qr`);
}

export function getPortalInstructions(): Promise<PortalInstructionsResponse> {
  return apiGet<PortalInstructionsResponse>('/api/portal/instructions');
}

export function getPortalInstruction(platform: string): Promise<PortalInstructionResponse> {
  return apiGet<PortalInstructionResponse>(
    `/api/portal/instructions/${encodeURIComponent(platform)}`,
  );
}
