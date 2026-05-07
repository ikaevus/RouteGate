import { apiGet } from '../../../shared/api/client';

export interface ManagerHealthResponse {
  status: string;
  service: string;
  timestamp: string;
}

export function getManagerHealth(): Promise<ManagerHealthResponse> {
  return apiGet<ManagerHealthResponse>('/api/admin/health');
}
