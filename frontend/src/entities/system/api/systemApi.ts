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

export function getSystemVersion(): Promise<SystemVersionResponse> {
  return apiGet<SystemVersionResponse>('/api/v1/system/version');
}

export function createUpdatePreflight(): Promise<UpdateJobCreateResponse> {
  return apiPost<Record<string, never>, UpdateJobCreateResponse>('/api/v1/system/update-jobs/preflight', {});
}

export function createUpdateDiscovery(): Promise<UpdateJobCreateResponse> {
  return apiPost<Record<string, never>, UpdateJobCreateResponse>('/api/v1/system/update-jobs/discovery', {});
}

export function createUpdateStage(discoveryJobId: string): Promise<UpdateJobCreateResponse> {
  return apiPost<{ discoveryJobId: string }, UpdateJobCreateResponse>('/api/v1/system/update-jobs/stage', { discoveryJobId });
}

export function createUpdateApply(stageJobId: string): Promise<UpdateJobCreateResponse> {
  return apiPost<{ stageJobId: string }, UpdateJobCreateResponse>('/api/v1/system/update-jobs/apply', { stageJobId });
}
