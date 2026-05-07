import { apiGet, apiPost } from '../../../shared/api/client';

export interface Agent {
  id: string;
  serverId: string;
  name: string;
  version: string;
  hostname: string;
  status: string;
  lastSeen: string;
  createdAt: string;
}

export interface ListAgentsResponse {
  items: Agent[];
}

export interface RegisterAgentRequest {
  serverId: string;
  name: string;
  version: string;
  hostname: string;
}

export interface RegisterAgentResponse {
  agent: Agent;
  token: string;
}

export interface HeartbeatRequest {
  agentId: string;
  version: string;
  hostname: string;
  status: string;
}

export function getAgents(): Promise<ListAgentsResponse> {
  return apiGet<ListAgentsResponse>('/api/admin/agents');
}

export function registerAgent(request: RegisterAgentRequest): Promise<RegisterAgentResponse> {
  return apiPost<RegisterAgentRequest, RegisterAgentResponse>('/api/agent/register', request);
}

export function sendAgentHeartbeat(request: HeartbeatRequest): Promise<{ status: string; timestamp: string }> {
  return apiPost<HeartbeatRequest, { status: string; timestamp: string }>(
    '/api/agent/heartbeat',
    request,
  );
}
