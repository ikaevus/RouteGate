import type { ClientConnection } from '../api/connectionApi.ts';

export interface ConnectionGroup extends ClientConnection {
  groupKey: string;
  details: ClientConnection[];
}

export function groupConnections(items: ClientConnection[]): ConnectionGroup[] {
  const groups = new Map<string, ConnectionGroup>();
  for (const item of items) {
    const groupKey = JSON.stringify([item.vpnAccountId, item.serverId, item.agentId ?? '']);
    const existing = groups.get(groupKey);
    if (existing) existing.details.push(item);
    else groups.set(groupKey, { ...item, groupKey, details: [item] });
  }
  return [...groups.values()].map((group) => {
    const exact = group.details.filter((item) => item.state === 'online' && item.confidence === 'exact');
    const latest = group.details.reduce((a, b) =>
      Date.parse(a.lastActivityAt ?? a.observedAt) >= Date.parse(b.lastActivityAt ?? b.observedAt) ? a : b);
    return {
      ...group,
      state: exact.length ? 'online' as const : 'recently_active' as const,
      confidence: exact.length ? 'exact' as const : 'heuristic' as const,
      connectionCount: exact.reduce((sum, item) => sum + item.connectionCount, 0),
      lastActivityAt: latest.lastActivityAt ?? latest.observedAt,
    };
  }).sort((a, b) => Number(b.state === 'online') - Number(a.state === 'online'));
}
