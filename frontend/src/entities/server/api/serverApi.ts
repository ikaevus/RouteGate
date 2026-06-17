import { apiGet } from '../../../shared/api/client';

export interface ServerAgent {
  id: string;
  hostname: string;
  os: string;
  arch: string;
  agentVersion: string;
  status: string;
  lastSeenAt?: string | null;
}

export interface Server {
  id: string;
  name: string;
  description?: string;
  location: string;
  provider: string;
  publicIp: string;
  privateIp?: string;
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
