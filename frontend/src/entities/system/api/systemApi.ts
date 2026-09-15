import { apiGet, apiPost } from '../../../shared/api/client';

export interface SystemManagerVersion {
  version: string;
  gitCommit: string;
  buildDate: string;
}

export interface SystemWebUiVersion {
  version: string;
}

export interface SystemDatabaseVersion {
  expectedSchemaVersion: number;
  appliedSchemaVersion?: string;
}

export interface SystemAgentCompatibilityVersion {
  protocolVersion: number;
  minimumProtocolVersion: number;
  recommendedAgentVersion: string;
}

export interface SystemUpdateVersion {
  status: string;
  channel: string;
  automaticUpdatesSupported: boolean;
}

export interface SystemVersionResponse {
  manager: SystemManagerVersion;
  webUi: SystemWebUiVersion;
  database: SystemDatabaseVersion;
  agentCompatibility: SystemAgentCompatibilityVersion;
  update: SystemUpdateVersion;
}

export interface UpdateJob {
  id: string;
  operation: 'preflight' | 'discovery' | 'stage' | 'apply';
  status: 'pending' | 'running' | 'succeeded' | 'failed';
  stage: 'preflight' | 'discovery' | 'stage' | 'apply';
  requestPayload: Record<string, unknown>;
  resultPayload: Record<string, unknown>;
  errorCode?: string;
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  completedAt?: string;
}

export interface UpdateJobCreateResponse {
  job: UpdateJob;
}

export interface UpdatePreflightResult {
  decision: 'proceed' | 'blocked';
  blockers: string[];
}

export interface UpdateDiscoveryResult {
  source: string;
  currentVersion: string;
  candidateVersion?: string;
  publishedAt?: string;
  runtimeOs: string;
  runtimeArch: string;
  availability: string;
  provenanceStatus: string;
  verificationRequired: string;
  missingAssets?: string[];
}

export interface UpdateStageResult {
  discoveryJobId: string;
  candidateVersion: string;
  verifiedVersion: string;
  verifiedCommit: string;
  expectedMigration: string;
  runtimeOs: string;
  runtimeArch: string;
  provenanceStatus: string;
  verification: string;
}

export interface MaintenanceCategory {
  id: string;
  scope: 'postgresql' | 'manager' | 'agent' | 'prometheus';
  recommended: boolean;
  selectable: boolean;
  retentionDays: number;
  blockedReason?: string;
  candidateCount: number;
  estimatedBytes?: number;
}

export interface MaintenanceInventory {
  analyzedAt: string;
  categories: MaintenanceCategory[];
}

export interface MaintenancePlanItem {
  categoryId: string;
  scope: string;
  retentionDays: number;
  cutoff: string;
  candidateCount: number;
  estimatedBytes?: number;
}

export interface MaintenanceProviderReport {
  categoryId: string;
  status: 'succeeded' | 'failed';
  deletedCount: number;
  reclaimedBytes?: number;
  remainingCount: number;
  errorCode?: string;
}

export interface MaintenanceReport {
  schemaVersion: number;
  status: 'succeeded' | 'failed';
  startedAt: string;
  completedAt: string;
  providers: MaintenanceProviderReport[];
}

export interface MaintenancePlan {
  id: string;
  mode: 'recommended' | 'advanced';
  status: 'planned' | 'running' | 'succeeded' | 'failed' | 'expired';
  selectedCategories: string[];
  payload: { schemaVersion: number; items: MaintenancePlanItem[] };
  createdAt: string;
  expiresAt: string;
  report?: MaintenanceReport;
  errorCode?: string;
}

export interface MaintenancePlanResponse {
  plan: MaintenancePlan;
  confirmationToken: string;
}

export function getSystemVersion(): Promise<SystemVersionResponse> {
  return apiGet<SystemVersionResponse>('/api/v1/system/version');
}

export function createUpdatePreflight(): Promise<UpdateJobCreateResponse> {
  return apiPost<never, UpdateJobCreateResponse>('/api/v1/system/update-jobs/preflight');
}

export function createUpdateDiscovery(): Promise<UpdateJobCreateResponse> {
  return apiPost<never, UpdateJobCreateResponse>('/api/v1/system/update-jobs/discovery');
}

export function createUpdateStage(discoveryJobId: string): Promise<UpdateJobCreateResponse> {
  return apiPost<{ discoveryJobId: string }, UpdateJobCreateResponse>('/api/v1/system/update-jobs/stage', { discoveryJobId });
}

export function createUpdateApply(stageJobId: string): Promise<UpdateJobCreateResponse> {
  return apiPost<{ stageJobId: string }, UpdateJobCreateResponse>('/api/v1/system/update-jobs/apply', { stageJobId });
}

export function getMaintenanceInventory(): Promise<MaintenanceInventory> {
  return apiGet<MaintenanceInventory>('/api/v1/system/maintenance');
}

export function createMaintenancePlan(
  mode: 'recommended' | 'advanced',
  categories: string[] = [],
): Promise<MaintenancePlanResponse> {
  return apiPost<{ mode: string; categories: string[] }, MaintenancePlanResponse>(
    '/api/v1/system/maintenance/plans',
    { mode, categories },
  );
}

export function executeMaintenancePlan(
  planId: string,
  confirmationToken: string,
): Promise<MaintenancePlan> {
  return apiPost<{ confirmationToken: string }, MaintenancePlan>(
    `/api/v1/system/maintenance/plans/${encodeURIComponent(planId)}/execute`,
    { confirmationToken },
  );
}
