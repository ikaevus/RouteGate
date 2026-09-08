import { apiDelete, apiGet, apiPatch, apiPost } from '../../../shared/api/client';

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
  id: string;
  name: string;
  description?: string | null;
  isDefault: boolean;
	defaultAction: RoutingRuleAction;
  rules?: RoutingProfileRule[];
  createdAt: string;
  updatedAt: string;
}

export interface ListRoutingProfilesResponse {
  items: RoutingProfile[];
}

export interface CreateRoutingProfileRequest {
  name: string;
  description: string;
  isDefault: boolean;
	defaultAction?: RoutingRuleAction;
}

export interface UpdateRoutingProfileRequest {
  name?: string;
  description?: string;
  isDefault?: boolean;
	defaultAction?: RoutingRuleAction;
}

export interface ManagedRuleSet {
  id: string;
  routingProfileId: string;
  name: string;
  provider: string;
  sourceUrl: string;
  sourceFormat: 'source';
  priority: number;
  action: RoutingRuleAction;
  enabled: boolean;
  refreshIntervalHours: number;
  status: 'pending' | 'healthy' | 'error';
  lastError?: string;
  ruleCount: number;
  lastRefreshAt?: string;
  lastSuccessfulAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ManagedRuleSetRequest {
  name: string;
  provider: string;
  sourceUrl: string;
  priority: number;
  action: RoutingRuleAction;
  enabled: boolean;
  refreshIntervalHours: number;
}

export interface RoutingDiagnosticResult {
  target: string;
  profileId: string;
  profileName: string;
  action: RoutingRuleAction;
  matched: boolean;
  matchedId?: string;
  matchedName?: string;
  source: 'manual' | 'managed_rule_set' | 'profile_default';
  priority?: number;
  precedence: string;
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

export function getManagedRuleSets(profileId?: string): Promise<{ items: ManagedRuleSet[] }> {
  const query = profileId ? `?profileId=${encodeURIComponent(profileId)}` : '';
  return apiGet<{ items: ManagedRuleSet[] }>(`/api/v1/managed-routing-rule-sets${query}`);
}

export function createManagedRuleSet(profileId: string, request: ManagedRuleSetRequest): Promise<ManagedRuleSet> {
  return apiPost<ManagedRuleSetRequest, ManagedRuleSet>(`/api/v1/routing-profiles/${encodeURIComponent(profileId)}/managed-rule-sets`, request);
}

export function updateManagedRuleSet(id: string, request: Partial<ManagedRuleSetRequest>): Promise<ManagedRuleSet> {
  return apiPatch<Partial<ManagedRuleSetRequest>, ManagedRuleSet>(`/api/v1/managed-routing-rule-sets/${encodeURIComponent(id)}`, request);
}

export function deleteManagedRuleSet(id: string): Promise<void> {
  return apiDelete(`/api/v1/managed-routing-rule-sets/${encodeURIComponent(id)}`);
}

export function refreshManagedRuleSet(id: string): Promise<ManagedRuleSet> {
  return apiPost<Record<string, never>, ManagedRuleSet>(`/api/v1/managed-routing-rule-sets/${encodeURIComponent(id)}/refresh`, {});
}

export function diagnoseRouting(profileId: string, target: string): Promise<RoutingDiagnosticResult> {
  return apiPost<{ target: string }, RoutingDiagnosticResult>(`/api/v1/routing-profiles/${encodeURIComponent(profileId)}/diagnostics`, { target });
}
