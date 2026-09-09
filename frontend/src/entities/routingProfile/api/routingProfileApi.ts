import { apiDelete, apiGet, apiPatch, apiPost, apiPut } from '../../../shared/api/client';

export type RoutingRuleAction = 'direct' | 'vpn' | 'block';

export interface RoutingProfileRule {
  id: string;
  routingProfileId: string;
  name: string;
  priority: number;
  action: RoutingRuleAction;
  domains?: string[];
  domainSuffixes?: string[];
  domainKeywords?: string[];
  ipCidrs?: string[];
  geoSites?: string[];
  geoIps?: string[];
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface RoutingProfile {
  defaultAction: RoutingRuleAction;
  managedSets?: ManagedRuleSet[];
  id: string;
  name: string;
  description?: string | null;
  isDefault: boolean;
  rules?: RoutingProfileRule[];
  createdAt: string;
  updatedAt: string;
}

export interface ListRoutingProfilesResponse {
  items: RoutingProfile[];
}

export interface CreateRoutingProfileRequest {
  defaultAction?: RoutingRuleAction;
  name: string;
  description: string;
  isDefault: boolean;
}

export interface UpdateRoutingProfileRequest {
  defaultAction?: RoutingRuleAction;
  name?: string;
  description?: string;
  isDefault?: boolean;
}

export interface CreateRoutingProfileRuleRequest {
  name: string;
  priority: number;
  action: RoutingRuleAction;
  domains: string[];
  domainSuffixes: string[];
  domainKeywords: string[];
  ipCidrs: string[];
  geoSites: string[];
  geoIps: string[];
  enabled: boolean;
}

export type UpdateRoutingProfileRuleRequest = Partial<CreateRoutingProfileRuleRequest>;

export function getRoutingProfiles(): Promise<ListRoutingProfilesResponse> {
  return apiGet<ListRoutingProfilesResponse>('/api/v1/routing-profiles');
}

export function getRoutingProfile(profileId: string): Promise<RoutingProfile> {
  return apiGet<RoutingProfile>(`/api/v1/routing-profiles/${encodeURIComponent(profileId)}`);
}

export function createRoutingProfile(
  request: CreateRoutingProfileRequest,
): Promise<RoutingProfile> {
  return apiPost<CreateRoutingProfileRequest, RoutingProfile>('/api/v1/routing-profiles', request);
}

export function updateRoutingProfile(
  profileId: string,
  request: UpdateRoutingProfileRequest,
): Promise<RoutingProfile> {
  return apiPatch<UpdateRoutingProfileRequest, RoutingProfile>(
    `/api/v1/routing-profiles/${encodeURIComponent(profileId)}`,
    request,
  );
}

export function deleteRoutingProfile(profileId: string): Promise<void> {
  return apiDelete(`/api/v1/routing-profiles/${encodeURIComponent(profileId)}`);
}

export function createRoutingProfileRule(
  profileId: string,
  request: CreateRoutingProfileRuleRequest,
): Promise<RoutingProfileRule> {
  return apiPost<CreateRoutingProfileRuleRequest, RoutingProfileRule>(
    `/api/v1/routing-profiles/${encodeURIComponent(profileId)}/rules`,
    request,
  );
}

export function updateRoutingProfileRule(
  profileId: string,
  ruleId: string,
  request: UpdateRoutingProfileRuleRequest,
): Promise<RoutingProfileRule> {
  return apiPatch<UpdateRoutingProfileRuleRequest, RoutingProfileRule>(
    `/api/v1/routing-profiles/${encodeURIComponent(profileId)}/rules/${encodeURIComponent(ruleId)}`,
    request,
  );
}

export function deleteRoutingProfileRule(profileId: string, ruleId: string): Promise<void> {
  return apiDelete(
    `/api/v1/routing-profiles/${encodeURIComponent(profileId)}/rules/${encodeURIComponent(ruleId)}`,
  );
}

export interface ManagedSetInput {
  name: string; provider: string; sourceUrl: string; priority: number;
  action: RoutingRuleAction; enabled: boolean; refreshHours: number;
}
export interface ManagedRuleSet extends ManagedSetInput {
  id: string; routingProfileId: string; snapshotSha256: string;
  lastAttemptAt: string | null; lastSuccessAt: string | null; lastError: string;
  createdAt: string; updatedAt: string;
}
export interface RuleSetProvider { id: string; name: string; url: string; action: RoutingRuleAction }
export interface RoutingDiagnostic {
  profileId: string; profileName: string; destination: string; resolvedIp?: string;
  action?: RoutingRuleAction; status: 'matched' | 'default' | 'indeterminate';
  winner?: { id: string; name: string; kind: 'manual' | 'managed'; provider?: string; priority: number; createdAt: string; snapshotSha256?: string };
  order?: number; reason: string;
}
const managedPath = (profileId: string, id?: string) => `/api/v1/routing-profiles/${encodeURIComponent(profileId)}/managed-sets${id ? `/${encodeURIComponent(id)}` : ''}`;
export const getRuleSetProviders = () => apiGet<{ items: RuleSetProvider[] }>('/api/v1/routing-rule-set-providers');
export const createManagedSet = (profileId: string, input: ManagedSetInput) => apiPost<ManagedSetInput, ManagedRuleSet>(managedPath(profileId), input);
export const updateManagedSet = (profileId: string, id: string, input: ManagedSetInput) => apiPut<ManagedSetInput, ManagedRuleSet>(managedPath(profileId, id), input);
export const deleteManagedSet = (profileId: string, id: string) => apiDelete(managedPath(profileId, id));
export const refreshManagedSet = (profileId: string, id: string) => apiPost<Record<string, never>, ManagedRuleSet>(`${managedPath(profileId, id)}/refresh`, {});
export const diagnoseRouting = (profileId: string, destination: string, resolvedIp: string) => apiPost<{ destination: string; resolvedIp: string }, RoutingDiagnostic>(`/api/v1/routing-profiles/${encodeURIComponent(profileId)}/diagnostics`, { destination, resolvedIp });
