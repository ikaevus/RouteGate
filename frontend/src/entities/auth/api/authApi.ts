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

export function login(request: LoginRequest): Promise<LoginResponse> {
  return apiPost<LoginRequest, LoginResponse>('/api/admin/auth/login', request);
}

export function logout(): Promise<{ status: string; timestamp: string }> {
  return apiPost<undefined, { status: string; timestamp: string }>('/api/admin/auth/logout');
}

export function getMe(): Promise<MeResponse> {
  return apiGet<MeResponse>('/api/admin/me');
}
