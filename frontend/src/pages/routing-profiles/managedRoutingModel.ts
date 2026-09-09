import type { ManagedRuleSet, ManagedSetInput } from '../../entities/routingProfile/api/routingProfileApi';

export function managedSetInput(source: ManagedRuleSet): ManagedSetInput {
  return { name: source.name, provider: source.provider, sourceUrl: source.sourceUrl, priority: source.priority,
    action: source.action, enabled: source.enabled, refreshHours: source.refreshHours };
}
export function canEnableManagedSet(source: Pick<ManagedRuleSet, 'lastSuccessAt' | 'snapshotSha256'>): boolean {
  return Boolean(source.lastSuccessAt && source.snapshotSha256);
}
export function managedSourceHealth(source: Pick<ManagedRuleSet, 'lastError' | 'lastSuccessAt' | 'snapshotSha256'>): 'failed' | 'healthy' | 'pending' {
  if (source.lastError) return 'failed';
  return canEnableManagedSet(source) ? 'healthy' : 'pending';
}
