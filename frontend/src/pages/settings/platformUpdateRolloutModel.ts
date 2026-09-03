import type {
  PlatformUpdateRollout,
  PlatformUpdateRolloutStatus,
} from '../../entities/system/api/platformUpdateRolloutApi';

export const MAX_PLATFORM_UPDATE_ROLLOUT_MEMBERS = 1024;

const canonicalUuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const canonicalUuidV4Pattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const canonicalVersionPattern = /^v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?(?:\+[0-9A-Za-z][0-9A-Za-z.-]*)?$/;

export type RolloutNextAction = 'start' | 'check' | 'continue';

export interface StoredRolloutCreateAttempt {
  idempotencyKey: string;
  targetVersion: string;
  serverIds: string[];
}

export function isCanonicalUuid(value: string): boolean {
  return canonicalUuidPattern.test(value);
}

export function isCanonicalRouteGateVersion(value: string): boolean {
  if (!canonicalVersionPattern.test(value)) {
    return false;
  }
  const payload = JSON.stringify({ schemaVersion: 1, targetVersion: value });
  return new TextEncoder().encode(payload).length <= 256;
}

export function isTerminalRolloutStatus(status: PlatformUpdateRolloutStatus): boolean {
  return status === 'succeeded' || status === 'failed' || status === 'outcome_unknown';
}

export function advanceFailureRequiresDurableRefresh(httpStatus?: number): boolean {
  if (httpStatus === undefined) {
    return true;
  }
  return ![400, 401, 403, 404, 405, 413, 415, 422].includes(httpStatus);
}

export function rolloutNextAction(rollout: PlatformUpdateRollout): RolloutNextAction | null {
  if (isTerminalRolloutStatus(rollout.status)) {
    return null;
  }
  if (rollout.status === 'pending') {
    return 'start';
  }
  if (rollout.entries.some((entry) => entry.status === 'updating')) {
    return 'check';
  }
  return 'continue';
}

export function moveSelectedServer(
  serverIds: string[],
  serverId: string,
  direction: -1 | 1,
): string[] {
  const index = serverIds.indexOf(serverId);
  const nextIndex = index + direction;
  if (index < 0 || nextIndex < 0 || nextIndex >= serverIds.length) {
    return serverIds;
  }
  const next = [...serverIds];
  [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
  return next;
}

export function appendSelectedServers(
  selectedServerIds: string[],
  candidateServerIds: string[],
): string[] {
  const next = [...selectedServerIds];
  const seen = new Set(next);
  for (const serverId of candidateServerIds) {
    if (next.length === MAX_PLATFORM_UPDATE_ROLLOUT_MEMBERS) {
      break;
    }
    if (!seen.has(serverId)) {
      next.push(serverId);
      seen.add(serverId);
    }
  }
  return next;
}

export function sameRolloutCreateRequest(
  attempt: StoredRolloutCreateAttempt,
  targetVersion: string,
  serverIds: string[],
): boolean {
  return attempt.targetVersion === targetVersion
    && attempt.serverIds.length === serverIds.length
    && attempt.serverIds.every((serverId, index) => serverId === serverIds[index]);
}

export function parseStoredRolloutCreateAttempt(raw: string | null): StoredRolloutCreateAttempt | null {
  if (!raw) {
    return null;
  }
  try {
    const value = JSON.parse(raw) as Partial<StoredRolloutCreateAttempt>;
    if (
      typeof value.idempotencyKey !== 'string'
      || !canonicalUuidV4Pattern.test(value.idempotencyKey)
      || typeof value.targetVersion !== 'string'
      || !isCanonicalRouteGateVersion(value.targetVersion)
      || !Array.isArray(value.serverIds)
      || value.serverIds.length === 0
      || value.serverIds.length > MAX_PLATFORM_UPDATE_ROLLOUT_MEMBERS
      || value.serverIds.some((serverId) => typeof serverId !== 'string' || !isCanonicalUuid(serverId))
      || new Set(value.serverIds).size !== value.serverIds.length
    ) {
      return null;
    }
    return {
      idempotencyKey: value.idempotencyKey,
      targetVersion: value.targetVersion,
      serverIds: [...value.serverIds],
    };
  } catch {
    return null;
  }
}
