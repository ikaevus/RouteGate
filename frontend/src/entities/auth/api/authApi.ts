import { apiGet, apiPost } from '../../../shared/api/client';

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
