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
}

export interface UpdateRoutingProfileRequest {
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
