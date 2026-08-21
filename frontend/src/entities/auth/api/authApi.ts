import { apiDelete, apiGet, apiPost } from '../../../shared/api/client';

export interface AuthUser {
  id: string;
  email: string;
  displayName: string;
  roles: string[];
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface LoginResponse {
  token: string;
  user: AuthUser;
}

export interface MeResponse {
  user: AuthUser;
}

export interface InitialSetupInspectRequest {
  token: string;
}

export interface InitialSetupInspectResponse {
  email: string;
  expires_at: string;
}

export interface InitialSetupCompleteRequest {
  token: string;
  new_password: string;
}

export interface ChangePasswordRequest {
  current_password: string;
  new_password: string;
}

export interface SecuritySession {
  id: string;
  current: boolean;
  ip_address?: string;
  user_agent?: string;
  created_at: string;
  last_used_at?: string;
  expires_at: string;
}

export interface SecuritySessionsResponse {
  sessions: SecuritySession[];
}

export interface SecurityEvent {
  id: string;
  action: string;
  result: string;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface SecurityEventsResponse {
  events: SecurityEvent[];
}

export function login(request: LoginRequest): Promise<LoginResponse> {
  return apiPost<LoginRequest, LoginResponse>('/api/admin/auth/login', request);
}

export function logout(): Promise<{ status: string; timestamp: string }> {
  return apiPost<undefined, { status: string; timestamp: string }>('/api/admin/auth/logout');
}

export function getMe(): Promise<MeResponse> {
  return apiGet<MeResponse>('/api/admin/me');
}

export function inspectInitialSetup(request: InitialSetupInspectRequest): Promise<InitialSetupInspectResponse> {
  return apiPost<InitialSetupInspectRequest, InitialSetupInspectResponse>('/api/v1/auth/initial-setup/inspect', request);
}

export function completeInitialSetup(request: InitialSetupCompleteRequest): Promise<LoginResponse> {
  return apiPost<InitialSetupCompleteRequest, LoginResponse>('/api/v1/auth/initial-setup/complete', request);
}

export function changePassword(request: ChangePasswordRequest): Promise<{ status: string; timestamp: string }> {
  return apiPost<ChangePasswordRequest, { status: string; timestamp: string }>('/api/v1/auth/change-password', request);
}

export function getSecuritySessions(): Promise<SecuritySessionsResponse> {
  return apiGet<SecuritySessionsResponse>('/api/v1/auth/sessions');
}

export function revokeSecuritySession(sessionId: string): Promise<void> {
  return apiDelete(`/api/v1/auth/sessions/${encodeURIComponent(sessionId)}`);
}

export function revokeOtherSecuritySessions(): Promise<{ status: string; timestamp: string }> {
  return apiPost<undefined, { status: string; timestamp: string }>('/api/v1/auth/sessions/revoke-others');
}

export function getSecurityEvents(): Promise<SecurityEventsResponse> {
  return apiGet<SecurityEventsResponse>('/api/v1/auth/security-events');
}

export function clearSecurityEvents(): Promise<void> {
  return apiDelete('/api/v1/auth/security-events');
}
