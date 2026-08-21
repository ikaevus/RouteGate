import { apiDelete, apiGet, apiPatch, apiPost, apiPut } from '../../../shared/api/client';

export type NodeGroupSelectionStrategy = 'priority' | 'weighted';
export type NodeGroupCandidateHealth = 'ready' | 'degraded' | 'unavailable';

export interface NodeGroupMember {
  nodeGroupId: string;
  serverId: string;
  serverName: string;
  protocol: string;
  deploymentRole: string;
  priority: number;
  weight: number;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface NodeGroup {
  id: string;
  name: string;
  description?: string;
  selectionStrategy: NodeGroupSelectionStrategy;
  memberCount: number;
  members?: NodeGroupMember[];
  createdAt: string;
  updatedAt: string;
}

export interface NodeGroupCandidate {
  serverId: string;
  serverName: string;
  protocol: string;
  priority: number;
  weight: number;
  memberEnabled: boolean;
  nodeStatus: string;
  agentStatus?: string;
  lastSeenAt?: string;
  load1?: number;
  logicalCpus?: number;
  loadPerCpu?: number;
  protocolSupported: boolean;
  runtimeState?: string;
  eligible: boolean;
  health: NodeGroupCandidateHealth;
  signals: string[];
}

export interface ListNodeGroupsResponse {
  items: NodeGroup[];
}

export interface ListNodeGroupCandidatesResponse {
  nodeGroupId: string;
  selectionStrategy: NodeGroupSelectionStrategy;
  candidates: NodeGroupCandidate[];
}

export interface CreateNodeGroupRequest {
  name: string;
  description: string;
  selectionStrategy: NodeGroupSelectionStrategy;
}

export type UpdateNodeGroupRequest = Partial<CreateNodeGroupRequest>;

export interface UpsertNodeGroupMemberRequest {
  priority: number;
  weight: number;
  enabled: boolean;
}

export function getNodeGroups(): Promise<ListNodeGroupsResponse> {
  return apiGet<ListNodeGroupsResponse>('/api/v1/node-groups');
}

export function getNodeGroup(groupId: string): Promise<NodeGroup> {
  return apiGet<NodeGroup>(`/api/v1/node-groups/${encodeURIComponent(groupId)}`);
}

export function createNodeGroup(request: CreateNodeGroupRequest): Promise<NodeGroup> {
  return apiPost<CreateNodeGroupRequest, NodeGroup>('/api/v1/node-groups', request);
}

export function updateNodeGroup(groupId: string, request: UpdateNodeGroupRequest): Promise<NodeGroup> {
  return apiPatch<UpdateNodeGroupRequest, NodeGroup>(`/api/v1/node-groups/${encodeURIComponent(groupId)}`, request);
}

export function deleteNodeGroup(groupId: string): Promise<void> {
  return apiDelete(`/api/v1/node-groups/${encodeURIComponent(groupId)}`);
}

export function upsertNodeGroupMember(
  groupId: string,
  serverId: string,
  request: UpsertNodeGroupMemberRequest,
): Promise<NodeGroupMember> {
  return apiPut<UpsertNodeGroupMemberRequest, NodeGroupMember>(
    `/api/v1/node-groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(serverId)}`,
    request,
  );
}

export function deleteNodeGroupMember(groupId: string, serverId: string): Promise<void> {
  return apiDelete(`/api/v1/node-groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(serverId)}`);
}

export function getNodeGroupCandidates(groupId: string): Promise<ListNodeGroupCandidatesResponse> {
  return apiGet<ListNodeGroupCandidatesResponse>(`/api/v1/node-groups/${encodeURIComponent(groupId)}/candidates`);
}
