import { apiGet, apiPost } from '../../../shared/api/client';

export interface Server {
  id: string;
  name: string;
  hostname: string;
  publicIp: string;
  location: string;
  provider: string;
  status: string;
  createdAt: string;
}

export interface ListServersResponse {
  items: Server[];
}

export interface CreateServerRequest {
  name: string;
  hostname: string;
  publicIp: string;
  location: string;
  provider: string;
}

export function getServers(): Promise<ListServersResponse> {
  return apiGet<ListServersResponse>('/api/admin/servers');
}

export function createServer(request: CreateServerRequest): Promise<Server> {
  return apiPost<CreateServerRequest, Server>('/api/admin/servers', request);
}
