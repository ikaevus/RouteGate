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
  available: boolean;
}

export interface DashboardTrafficResponse {
  generatedAt: string;
  monthStart: string;
  last30DaysStart: string;
  last30DaysEnd: string;
  server24hFrom: string;
  server24hTo: string;
  monthlyAvailable: boolean;
  dailyAvailable: boolean;
  monthly: DashboardTrafficTotals;
  daily: DashboardDailyTraffic[];
  servers: DashboardServerTraffic[];
}

export interface DashboardNodeLocation {
  location: string;
  count: number;
}

export interface DashboardServerLoad {
  serverId: string;
  load1?: number;
  load5?: number;
  load15?: number;
  logicalCpus?: number;
  collectedAt?: string;
}

export interface DashboardNodeDistribution {
  totalServers: number;
  locations: DashboardNodeLocation[];
  serverLoads: DashboardServerLoad[];
}

export function getDashboardActivity(): Promise<DashboardActivityResponse> {
  return apiGet<DashboardActivityResponse>('/api/v1/dashboard/activity');
}

export function getDashboardTraffic(): Promise<DashboardTrafficResponse> {
  return apiGet<DashboardTrafficResponse>('/api/v1/dashboard/traffic');
}

export function getDashboardNodes(): Promise<DashboardNodeDistribution> {
  return apiGet<DashboardNodeDistribution>('/api/v1/dashboard/nodes');
}
