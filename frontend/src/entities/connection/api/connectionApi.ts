import { apiGet } from '../../../shared/api/client';

export type ConnectionState = 'online' | 'recently_active';

export interface ClientConnection {
  vpnAccountId: string;
  accountName: string;
  email?: string;
  serverId: string;
  serverName: string;
  agentId?: string;
  agentName?: string;
  protocol: string;
  state: ConnectionState;
  connectionCount: number;
  source: string;
  confidence: 'exact' | 'heuristic';
  connectedAt?: string;
  lastActivityAt?: string;
  observedAt: string;
}

export interface ClientConnectionsResponse {
  generatedAt: string;
  summary: {
    onlineUsers: number;
    onlineConnections: number;
    recentlyActiveUsers: number;
  };
  items: ClientConnection[];
}

export function getClientConnections(limit = 100): Promise<ClientConnectionsResponse> {
  return apiGet<ClientConnectionsResponse>(`/api/v1/connections?limit=${limit}`);
}

