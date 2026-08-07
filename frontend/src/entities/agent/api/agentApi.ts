import { apiGet } from '../../../shared/api/client';

export interface Agent {
  id: string;
  serverId: string;
  name: string;
  agentVersion: string;
  version: string;
  protocolVersion?: number;
  compatibility?: {
    status: string;
    message?: string;
  };
  hostname: string;
  status: string;
  lastSeen: string;
  createdAt: string;
}

export interface ListAgentsResponse {
  items: Agent[];
}

export function getAgents(): Promise<ListAgentsResponse> {
  return apiGet<ListAgentsResponse>('/api/v1/agents');
}
