import { apiGet, apiPost } from '../../../shared/api/client';

export type PlatformUpdateRolloutStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'outcome_unknown';

export type PlatformUpdateRolloutEntryStatus =
  | 'queued'
  | 'waiting'
  | 'updating'
  | 'healthy'
  | 'failed'
  | 'outcome_unknown'
  | 'skipped';

export type PlatformUpdateRolloutStepAction =
  | 'mutation_admitted'
  | 'mutation_in_progress'
  | 'waiting_health'
  | 'node_healthy'
  | 'rollout_succeeded'
  | 'rollout_failed'
  | 'outcome_unknown'
  | 'no_change';

export interface PlatformUpdateRolloutEntry {
  serverId: string;
  position: number;
  status: PlatformUpdateRolloutEntryStatus;
  planningBlockers: string[];
  jobId?: string;
  completedAt?: string;
  blockerCode?: string;
}

export interface PlatformUpdateRollout {
  id: string;
  targetVersion: string;
  status: PlatformUpdateRolloutStatus;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  errorCode?: string;
  entries: PlatformUpdateRolloutEntry[];
}

export interface PlatformUpdateRolloutResponse {
  rollout: PlatformUpdateRollout;
}

export interface CreatePlatformUpdateRolloutRequest {
  targetVersion: string;
  serverIds: string[];
}

export interface AdvancePlatformUpdateRolloutResponse {
  rolloutId: string;
  rolloutStatus: PlatformUpdateRolloutStatus;
  serverId?: string;
  jobId?: string;
  action: PlatformUpdateRolloutStepAction;
  waitingReason?: string;
  errorCode?: string;
  blockerCode?: string;
}

export function createPlatformUpdateRollout(
  request: CreatePlatformUpdateRolloutRequest,
  idempotencyKey: string,
): Promise<PlatformUpdateRolloutResponse> {
  return apiPost<CreatePlatformUpdateRolloutRequest, PlatformUpdateRolloutResponse>(
    '/api/v1/platform-update-rollouts',
    request,
    { 'Idempotency-Key': idempotencyKey },
  );
}

export function getPlatformUpdateRollout(rolloutId: string): Promise<PlatformUpdateRolloutResponse> {
  return apiGet<PlatformUpdateRolloutResponse>(
    `/api/v1/platform-update-rollouts/${encodeURIComponent(rolloutId)}`,
  );
}

export function advancePlatformUpdateRollout(
  rolloutId: string,
): Promise<AdvancePlatformUpdateRolloutResponse> {
  return apiPost<never, AdvancePlatformUpdateRolloutResponse>(
    `/api/v1/platform-update-rollouts/${encodeURIComponent(rolloutId)}/advance`,
  );
}
