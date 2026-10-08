import type { VpnClientConnectionStatus } from '../../entities/vpnAccount/api/vpnAccountApi';

export type AccountAccessReadiness = VpnClientConnectionStatus | 'checking' | 'check_failed';
export type AccountAccessNextStep = 'assign_node' | 'apply' | 'investigate' | 'add_device' | 'wait' | null;

/** A fresh subscription is not ready to import until the Manager confirms it.
 * Do not confuse an active bearer token with deployed account credentials.
 */
export function mayIssueFreshAccess(readiness: AccountAccessReadiness): boolean {
  return readiness === 'ready';
}

/** Keep onboarding's next action aligned with the authenticated /client-profile readiness check. */
export function accountAccessNextStep(
  hasAssignedNode: boolean,
  readiness: AccountAccessReadiness,
  needsDevice: boolean,
): AccountAccessNextStep {
  if (!hasAssignedNode || readiness === 'unassigned') return 'assign_node';
  if (readiness === 'awaiting_apply' || readiness === 'awaiting_first_apply') return 'apply';
  if (readiness === 'unavailable' || readiness === 'check_failed') return 'investigate';
  if (readiness === 'checking') return 'wait';
  if (readiness === 'ready' && needsDevice) return 'add_device';
  return null;
}
