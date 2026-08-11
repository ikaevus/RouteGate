import { apiGet } from '../../../shared/api/client';

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

export function getSystemVersion(): Promise<SystemVersionResponse> {
  return apiGet<SystemVersionResponse>('/api/v1/system/version');
}
