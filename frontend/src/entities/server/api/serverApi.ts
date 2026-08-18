import { apiDelete, apiGet, apiPatch, apiPost, apiPut } from '../../../shared/api/client';

export type DeploymentRole = 'management' | 'vpn' | 'hybrid';

export interface ServerAgent {
  id: string;
  hostname?: string | null;
  os?: string | null;
  arch?: string | null;
  agentVersion: string;
  status: string;
  lastSeenAt?: string | null;
  capabilities?: Record<string, unknown>;
}

export interface RegistrationTokenResponse {
  serverId: string;
  registrationToken: string;
  expiresAt: string;
  managerUrl?: string;
  bootstrapCommand?: string;
}

export type NodeConnectionState = 'not_applicable' | 'awaiting_agent' | 'online' | 'offline';
export type NodeCapabilityStatus = 'not_applicable' | 'not_reported' | 'compatible' | 'incompatible';

export interface NodeInventorySummary {
  connectionState: NodeConnectionState;
  capabilityStatus: NodeCapabilityStatus;
  nextAction: 'none' | 'install_agent' | 'restore_connection' | 'review_compatibility' | 'review_capabilities';
  capabilitySchemaVersion?: number;
  managedAdapterCount: number;
}

export interface ValidationResult {
  valid: boolean;
  errors: string[];
  warnings: string[];
}

export interface ConfigVersion {
  id: string;
  serverId: string;
  version: number;
  configHash: string;
  status: string;
  renderedConfig?: Record<string, unknown>;
  createdAt: string;
  appliedAt?: string | null;
  pinned: boolean;
}

export interface ConfigApplyJob {
  id: string;
  serverId: string;
  agentId?: string;
  configVersionId: string;
  action: string;
  status: string;
  requestPayload: Record<string, unknown>;
  resultPayload: Record<string, unknown>;
  errorMessage?: string;
  createdAt: string;
  updatedAt: string;
  startedAt?: string | null;
  completedAt?: string | null;
}

export interface RenderConfigResponse {
  configVersion: ConfigVersion;
  validationResult: ValidationResult;
}

export interface ValidateConfigResponse {
  configVersion: ConfigVersion;
  validationResult: ValidationResult;
}

export interface ApplyConfigResponse {
  job: ConfigApplyJob;
}

export interface ListConfigVersionsResponse {
  items: ConfigVersion[];
  currentConfigVersionId?: string | null;
}

export interface ListConfigApplyJobsResponse {
  items: ConfigApplyJob[];
}

export interface ProtocolSettingsResponse {
  serverId: string;
  protocol: string;
  vless: {
    port: number;
    flow?: string;
    network?: string;
  };
  reality: {
    enabled: boolean;
    publicKey?: string;
    shortId?: string;
    serverName?: string;
  };
	wireGuard: {
		port: number;
		address: string;
		dns: string;
		publicKey?: string;
		ready: boolean;
	};
  updatedAt: string;
}

export interface UpdateProtocolSettingsRequest {
	protocol?: 'vless' | 'wireguard';
  vlessPort: number;
  vlessFlow: string;
  vlessNetwork: string;
  realityPublicKey?: string;
  realityShortId: string;
  realityServerName: string;
	wireGuardPort?: number;
	wireGuardAddress?: string;
	wireGuardDns?: string;
}

export interface ServerRoutingProfile {
  id: string;
  name: string;
  description?: string | null;
  isDefault: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ServerRoutingProfileAssignment {
  serverId: string;
  routingProfile?: ServerRoutingProfile | null;
  createdAt?: string | null;
  updatedAt?: string | null;
}

export interface AssignServerRoutingProfileRequest {
  routingProfileId: string;
}

export interface Server {
  id: string;
  name: string;
  deploymentRole: DeploymentRole;
  description?: string | null;
  location?: string | null;
  provider?: string | null;
  publicIp?: string | null;
  privateIp?: string | null;
  status: string;
  createdAt: string;
  updatedAt: string;
  agent?: ServerAgent | null;
  inventory: NodeInventorySummary;
}

export interface ListServersResponse {
  items: Server[];
}

export interface CreateServerRequest {
  name: string;
  deploymentRole?: DeploymentRole;
  description?: string;
  location?: string;
  provider?: string;
  publicIp: string;
}

export interface UpdateServerRequest {
  name?: string;
  description?: string;
  location?: string;
  provider?: string;
  publicIp?: string;
}

export function getServers(): Promise<ListServersResponse> {
  return apiGet<ListServersResponse>('/api/v1/servers');
}

export function getServer(serverId: string): Promise<Server> {
  return apiGet<Server>(`/api/v1/servers/${encodeURIComponent(serverId)}`);
}

export function createServer(request: CreateServerRequest): Promise<Server> {
  return apiPost<CreateServerRequest, Server>('/api/v1/servers', request);
}

export function updateServer(
  serverId: string,
  request: UpdateServerRequest,
): Promise<Server> {
  return apiPatch<UpdateServerRequest, Server>(
    `/api/v1/servers/${encodeURIComponent(serverId)}`,
    request,
  );
}

export function deleteServer(serverId: string): Promise<void> {
  return apiDelete(`/api/v1/servers/${encodeURIComponent(serverId)}`);
}

export function createServerRegistrationToken(
  serverId: string,
): Promise<RegistrationTokenResponse> {
  return apiPost<undefined, RegistrationTokenResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/registration-token`,
  );
}

export function getProtocolSettings(serverId: string): Promise<ProtocolSettingsResponse> {
  return apiGet<ProtocolSettingsResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/protocol-settings`,
  );
}

export function updateProtocolSettings(
  serverId: string,
  request: UpdateProtocolSettingsRequest,
): Promise<ProtocolSettingsResponse> {
  return apiPatch<UpdateProtocolSettingsRequest, ProtocolSettingsResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/protocol-settings`,
    request,
  );
}

export function configureRecommendedProtocolSettings(
  serverId: string,
): Promise<ProtocolSettingsResponse> {
  return apiPost<undefined, ProtocolSettingsResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/protocol-settings/recommended`,
  );
}

export function generateRealityKeypair(serverId: string): Promise<ProtocolSettingsResponse> {
  return apiPost<undefined, ProtocolSettingsResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/protocol-settings/reality-keypair`,
  );
}

export function configureRecommendedWireGuard(
	serverId: string,
): Promise<ProtocolSettingsResponse> {
	return apiPost<undefined, ProtocolSettingsResponse>(
		`/api/v1/servers/${encodeURIComponent(serverId)}/protocol-settings/wireguard-recommended`,
	);
}

export function getServerRoutingProfile(
  serverId: string,
): Promise<ServerRoutingProfileAssignment> {
  return apiGet<ServerRoutingProfileAssignment>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/routing-profile`,
  );
}

export function assignServerRoutingProfile(
  serverId: string,
  request: AssignServerRoutingProfileRequest,
): Promise<ServerRoutingProfileAssignment> {
  return apiPut<AssignServerRoutingProfileRequest, ServerRoutingProfileAssignment>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/routing-profile`,
    request,
  );
}

export function clearServerRoutingProfile(serverId: string): Promise<void> {
  return apiDelete(`/api/v1/servers/${encodeURIComponent(serverId)}/routing-profile`);
}

export function getConfigVersions(serverId: string): Promise<ListConfigVersionsResponse> {
  return apiGet<ListConfigVersionsResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/versions`,
  );
}

export function renderConfig(serverId: string): Promise<RenderConfigResponse> {
  return apiPost<undefined, RenderConfigResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/render`,
  );
}

export function validateConfigVersion(
  serverId: string,
  versionId: string,
): Promise<ValidateConfigResponse> {
  return apiPost<undefined, ValidateConfigResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/versions/${encodeURIComponent(versionId)}/validate`,
  );
}

export function applyConfigVersion(
  serverId: string,
  versionId: string,
): Promise<ApplyConfigResponse> {
  return apiPost<undefined, ApplyConfigResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/versions/${encodeURIComponent(versionId)}/apply`,
  );
}

export function reapplyConfigVersion(
  serverId: string,
  versionId: string,
): Promise<ApplyConfigResponse> {
  return apiPost<undefined, ApplyConfigResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/versions/${encodeURIComponent(versionId)}/reapply`,
  );
}

export function deleteConfigVersion(serverId: string, versionId: string): Promise<void> {
  return apiDelete(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/versions/${encodeURIComponent(versionId)}`,
  );
}

export function pinConfigVersion(serverId: string, versionId: string): Promise<ConfigVersion> {
  return apiPost<undefined, ConfigVersion>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/versions/${encodeURIComponent(versionId)}/pin`,
  );
}

export function unpinConfigVersion(serverId: string, versionId: string): Promise<void> {
  return apiDelete(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/versions/${encodeURIComponent(versionId)}/pin`,
  );
}

export function getConfigApplyJobs(serverId: string): Promise<ListConfigApplyJobsResponse> {
  return apiGet<ListConfigApplyJobsResponse>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/apply-jobs`,
  );
}
