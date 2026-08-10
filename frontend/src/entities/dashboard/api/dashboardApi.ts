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

export interface DashboardTrafficTotals {
  rxBytes: number;
  txBytes: number;
  totalBytes: number;
}

export interface DashboardDailyTraffic extends DashboardTrafficTotals {
  date: string;
}

export interface DashboardServerTraffic extends DashboardTrafficTotals {
  serverId: string;
  serverName: string;
}

export interface DashboardTrafficResponse {
  generatedAt: string;
  monthStart: string;
  last30DaysStart: string;
  last30DaysEnd: string;
  server24hFrom: string;
  server24hTo: string;
  monthly: DashboardTrafficTotals;
  daily: DashboardDailyTraffic[];
  servers: DashboardServerTraffic[];
}

export function getDashboardActivity(): Promise<DashboardActivityResponse> {
  return apiGet<DashboardActivityResponse>('/api/v1/dashboard/activity');
}

export function getDashboardTraffic(): Promise<DashboardTrafficResponse> {
  return apiGet<DashboardTrafficResponse>('/api/v1/dashboard/traffic');
}
