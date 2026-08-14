import { apiDelete, apiGet } from '../../../shared/api/client';
import type { ConfigApplyJob } from './serverApi';

export interface ConfigApplyJobPage {
  items: ConfigApplyJob[];
  total: number;
  limit: number;
  offset: number;
}

export function getConfigApplyJobsPage(
  serverId: string,
  limit: number,
  offset: number,
): Promise<ConfigApplyJobPage> {
  const query = new URLSearchParams({
    limit: String(limit),
    offset: String(offset),
  });
  return apiGet<ConfigApplyJobPage>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/apply-jobs?${query.toString()}`,
  );
}

export function clearCompletedConfigApplyJobs(serverId: string): Promise<void> {
  return apiDelete(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/apply-jobs/completed`,
  );
}
