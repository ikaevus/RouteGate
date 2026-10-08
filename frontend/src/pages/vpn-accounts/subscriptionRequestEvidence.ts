import type { VpnAccountDeviceAccess } from '../../entities/vpnAccount/api/vpnAccountDeviceApi';

/**
 * Device activity is tied to the currently active subscription bearer,
 * not the device lifetime. A rotated/revoked bearer must never inherit a
 * "refreshed" indicator from its old URL.
 *
 * Even an observed request says nothing about successful import, activation,
 * endpoint connectivity or VPN traffic.
 */
export type SubscriptionRequestEvidence = 'no_active_link' | 'not_observed' | 'observed' | 'unknown';

export function subscriptionRequestEvidence(access: Pick<VpnAccountDeviceAccess, 'hasActiveToken' | 'tokenLastUsedAt'>): SubscriptionRequestEvidence {
  if (!access.hasActiveToken) return 'no_active_link';
  if (!access.tokenLastUsedAt) return 'not_observed';
  return Number.isFinite(Date.parse(access.tokenLastUsedAt)) ? 'observed' : 'unknown';
}
