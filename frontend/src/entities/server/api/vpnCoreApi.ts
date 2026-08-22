import { apiGet, apiPost } from '../../../shared/api/client';

export type VPNCoreOperation = 'start' | 'stop' | 'restart';
export type VPNRuntimeInstallationOperation =
  | 'install_sing_box'
  | 'install_wireguard'
  | 'install_hysteria2'
  | 'install_mtg';

export interface VPNCoreOperationJob {
  id: string;
  serverId: string;
  agentId?: string;
  kind: 'vpn_core_service' | 'vpn_core_install';
  operation: VPNCoreOperation | VPNRuntimeInstallationOperation;
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

export function getVPNCoreOperation(
  serverId: string,
  jobId: string,
): Promise<VPNCoreOperationJob> {
  return apiGet<VPNCoreOperationJob>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/vpn-core/operations/${encodeURIComponent(jobId)}`,
  );
}

export function createVPNCoreInstallation(
  serverId: string,
  operation: VPNRuntimeInstallationOperation = 'install_sing_box',
): Promise<CreateVPNCoreOperationResponse> {
  return apiPost<{ operation: VPNRuntimeInstallationOperation }, CreateVPNCoreOperationResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/vpn-core/installations`,
    { operation },
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
