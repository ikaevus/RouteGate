import { apiGet, apiPost } from '../../../shared/api/client';

export type VPNCoreOperation = 'start' | 'stop' | 'restart';

export interface VPNCoreOperationJob {
  id: string;
  serverId: string;
  agentId?: string;
  kind: 'vpn_core_service' | 'vpn_core_install';
  operation: VPNCoreOperation | 'install_sing_box';
  status: 'pending' | 'in_progress' | 'succeeded' | 'failed';
  createdAt: string;
  startedAt?: string | null;
  completedAt?: string | null;
  errorMessage?: string;
  resultPayload?: Record<string, unknown>;
}

interface CreateVPNCoreOperationResponse {
  job: VPNCoreOperationJob;
}

export function createVPNCoreOperation(
  serverId: string,
  operation: VPNCoreOperation,
): Promise<CreateVPNCoreOperationResponse> {
  return apiPost<{ operation: VPNCoreOperation }, CreateVPNCoreOperationResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/vpn-core/operations`,
    { operation },
  );
}

export function createVPNCoreInstallation(
  serverId: string,
): Promise<CreateVPNCoreOperationResponse> {
  return apiPost<never, CreateVPNCoreOperationResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/vpn-core/installations`,
  );
}

export function getVPNCoreInstallation(
  serverId: string,
  jobId: string,
): Promise<VPNCoreOperationJob> {
  return apiGet<VPNCoreOperationJob>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/vpn-core/installations/${encodeURIComponent(jobId)}`,
  );
}
