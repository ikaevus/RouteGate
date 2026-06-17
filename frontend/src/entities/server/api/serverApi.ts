import { apiGet, apiPost } from '../../../shared/api/client';

export interface ServerAgent {
  id: string;
  hostname?: string | null;
  os?: string | null;
  arch?: string | null;
  agentVersion: string;
  status: string;
  lastSeenAt?: string | null;
  capabilities?: Record<string, unknown>;
}

export interface RegistrationTokenResponse {
  serverId: string;
  registrationToken: string;
  expiresAt: string;
}

export interface Server {
  id: string;
  name: string;
  description?: string | null;
  location?: string | null;
  provider?: string | null;
  publicIp?: string | null;
  privateIp?: string | null;
  status: string;
  createdAt: string;
  updatedAt: string;
  agent?: ServerAgent | null;
}

export interface ListServersResponse {
  items: Server[];
}

export function getServers(): Promise<ListServersResponse> {
  return apiGet<ListServersResponse>('/api/v1/servers');
}

export function getServer(serverId: string): Promise<Server> {
  return apiGet<Server>(`/api/v1/servers/${encodeURIComponent(serverId)}`);
}

export function createServerRegistrationToken(
  serverId: string,
): Promise<RegistrationTokenResponse> {
  return apiPost<undefined, RegistrationTokenResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/registration-token`,
  );
}
