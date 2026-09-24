import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { getServers } from '../../entities/server/api/serverApi';
import { getVpnAccountLegacySubscriptionAccess, getVpnAccountRoutingPolicy, getVpnAccountTraffic } from '../../entities/vpnAccount/api/vpnAccountApi';
import { listVpnAccountDevices } from '../../entities/vpnAccount/api/vpnAccountDeviceApi';
import type { ManagedVpnAccount } from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { t } from '../../shared/i18n/i18n';
import { accountWorkspaceHref, type AccountSection } from './accountWorkspace';

function formatBytes(bytes: number): string {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

export function AccountOverview({ account }: { account: ManagedVpnAccount }) {
  const [searchParams] = useSearchParams();
  const accountId = account.id;
  const servers = useQuery({ queryKey: ['servers'], queryFn: getServers });
  const devices = useQuery({
    queryKey: ['vpn-account-devices', accountId],
    queryFn: () => listVpnAccountDevices(accountId),
  });
  const routing = useQuery({
    queryKey: ['vpn-account-routing-policy', accountId],
    queryFn: () => getVpnAccountRoutingPolicy(accountId),
  });
  const traffic = useQuery({
    queryKey: ['vpn-account-traffic', accountId],
    queryFn: () => getVpnAccountTraffic(accountId),
  });
  const legacyAccess = useQuery({
    queryKey: ['vpn-account-legacy-access', accountId],
    queryFn: () => getVpnAccountLegacySubscriptionAccess(accountId),
  });
  const href = (section: AccountSection) => accountWorkspaceHref(accountId, section, searchParams);
  const serverName = servers.data?.items.find((server) => server.id === account.serverId)?.name;
  const activeDevices = devices.data?.items.filter(({ device }) => device.status === 'active').length;
  const needsServer = (account.status === 'active' || account.status === 'created') && !account.serverId;
  const needsDevice = account.status === 'active' && Boolean(account.serverId)
    && devices.isSuccess && activeDevices === 0
    && legacyAccess.isSuccess && !legacyAccess.data.hasActiveToken;
  const accessHref = href('access');
  const addDeviceHref = `${accessHref}${accessHref.includes('?') ? '&' : '?'}addDevice=1`;

  return (
    <div className="vpn-account-overview">
      {(needsServer || needsDevice) && (
        <div className="vpn-account-next-action" role="status">
          <strong>{t('accountWorkspace.attention')}</strong>
          <p>{needsServer ? t('accountWorkspace.assignServerHint') : t('accountWorkspace.addDeviceHint')}</p>
          <Link className="small-button" to={needsServer ? href('routing') : addDeviceHref}>
            {needsServer ? t('accountWorkspace.assignServer') : t('accountWorkspace.addDevice')}
          </Link>
        </div>
      )}

      <div className="vpn-account-overview-grid">
        <Link className="panel vpn-account-summary" to={href('routing')}>
          <span className="vpn-account-summary-label">{t('accountWorkspace.server')}</span>
          <strong>{account.serverId ? (serverName || (servers.isError ? t('accountWorkspace.loadError') : account.serverId)) : t('accountWorkspace.unassigned')}</strong>
          <span>{t('accountWorkspace.routing')} →</span>
        </Link>
        <Link className="panel vpn-account-summary" to={href('access')}>
          <span className="vpn-account-summary-label">{t('accountWorkspace.devices')}</span>
          <strong>{devices.isError ? t('accountWorkspace.loadError') : devices.isLoading ? t('common.loading') : activeDevices}</strong>
          <span>{t('accountWorkspace.access')} →</span>
        </Link>
        <Link className="panel vpn-account-summary" to={href('routing')}>
          <span className="vpn-account-summary-label">{t('accountWorkspace.routingProfile')}</span>
          <strong>{routing.isError ? t('accountWorkspace.loadError') : routing.isLoading ? t('common.loading') : routing.data?.effectiveRoutingProfile?.name || t('accountWorkspace.none')}</strong>
          <span>{t('accountWorkspace.routing')} →</span>
        </Link>
        <Link className="panel vpn-account-summary" to={href('traffic')}>
          <span className="vpn-account-summary-label">{t('accountWorkspace.traffic')}</span>
          <strong>{traffic.isError ? t('accountWorkspace.loadError') : traffic.isLoading ? t('common.loading') : traffic.data ? formatBytes(traffic.data.usage.totalBytes) : t('common.notAvailable')}</strong>
          <span>{traffic.data?.limit?.monthlyLimitBytes ? `${t('accountWorkspace.limit')}: ${formatBytes(traffic.data.limit.monthlyLimitBytes)}` : t('accountWorkspace.trafficDetails')} →</span>
        </Link>
      </div>
      <Link className="vpn-account-overview-protocols text-link" to={href('protocols')}>
        {t('accountWorkspace.protocols')} →
      </Link>
    </div>
  );
}
