import { apiGet } from '../../../shared/api/client';

export interface DashboardRecentDeployment {
  id: string;
  serverId: string;
  serverName: string;
  configVersionId: string;
  configVersion: number;
  action: string;
  status: string;
  createdAt: string;
  completedAt?: string;
}

export interface DashboardRecentAuditEvent {
  id: string;
  actor: string;
  actorType: string;
  action: string;
  resourceType: string;
  resourceId?: string;
  result: string;
  createdAt: string;
}

export interface DashboardActivityResponse {
  recentDeployments: DashboardRecentDeployment[];
  recentAuditEvents: DashboardRecentAuditEvent[];
}

export function getDashboardActivity(): Promise<DashboardActivityResponse> {
  return apiGet<DashboardActivityResponse>('/api/v1/dashboard/activity');
}
