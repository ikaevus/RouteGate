import type { ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getMe } from '../../entities/auth/api/authApi';
import { getAgents } from '../../entities/agent/api/agentApi';
import {
  getDashboardActivity,
  getDashboardTraffic,
  type DashboardDailyTraffic,
  type DashboardRecentAuditEvent,
  type DashboardRecentDeployment,
} from '../../entities/dashboard/api/dashboardApi';
import { getServers } from '../../entities/server/api/serverApi';
import { getManagerHealth } from '../../entities/health/api/healthApi';
import { getPagedVpnAccounts } from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { t, translateStatus } from '../../shared/i18n/i18n';
import { StatusBadge } from '../../shared/ui/StatusBadge';
import { GettingStartedWidget } from './GettingStartedWidget';
import './DashboardPage.css';

function formatDate(value?: string | null): string {
  if (!value) {
    return t('common.notAvailable');
  }

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatTrafficDate(value: string): string {
  const date = new Date(`${value}T00:00:00Z`);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleDateString(undefined, { day: 'numeric', month: 'short', timeZone: 'UTC' });
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) {
    return t('common.notAvailable');
  }
  if (bytes === 0) {
    return '0 B';
  }

  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  const unitIndex = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / (1024 ** unitIndex);
  const maximumFractionDigits = unitIndex === 0 || value >= 100 ? 0 : value >= 10 ? 1 : 2;

  return `${value.toLocaleString(undefined, { maximumFractionDigits })} ${units[unitIndex]}`;
}

function getRegionCountryCode(region: string): string | null {
  return region.match(/,\s*([A-Z]{2})$/)?.[1] ?? null;
}

function getActorInitial(actor: string): string {
  const normalized = actor.trim();
  return normalized === '' ? '?' : normalized.slice(0, 1).toUpperCase();
}

function formatAuditResource(event: DashboardRecentAuditEvent): string {
  return event.resourceId ? `${event.resourceType} · ${event.resourceId}` : event.resourceType;
}

function KpiWidget({ title, value, meta, tone, icon }: { title: string; value: string; meta: string; tone: string; icon: string }) {
  return (
    <div className={`dashboard-widget kpi-widget kpi-widget-${tone}`}>
      <div>
        <div className="kpi-title"><span className="kpi-dot" />{title}</div>
        <div className="kpi-value">{value}</div>
        <div className="kpi-meta">{meta}</div>
      </div>
      <div className="kpi-icon" aria-hidden="true">{icon}</div>
    </div>
  );
}

function WidgetPanel({ title, subtitle, children, className = '', action }: { title: string; subtitle?: string; children: ReactNode; className?: string; action?: ReactNode }) {
  return (
    <section className={`dashboard-widget ${className}`}>
      <div className="dashboard-widget-header">
        <div>
          <h2>{title}</h2>
          {subtitle && <span>{subtitle}</span>}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

function UnavailableWidget({ title, subtitle, className }: { title: string; subtitle?: string; className: string }) {
  return (
    <WidgetPanel title={title} subtitle={subtitle} className={className}>
      <p className="empty-state">{t('common.notAvailable')}</p>
    </WidgetPanel>
  );
}

function InfrastructureHealthWidget({
  managerHealthy,
  serversCount,
  agentsCount,
  serversAvailable,
  agentsAvailable,
}: {
  managerHealthy: boolean;
  serversCount: number;
  agentsCount: number;
  serversAvailable: boolean;
  agentsAvailable: boolean;
}) {
  const rows = [
    [t('dashboard.managerApi'), managerHealthy ? 'healthy' : 'warning', managerHealthy ? t('dashboard.managerOnline') : t('common.notAvailable')],
    [t('dashboard.servers'), serversAvailable ? (serversCount > 0 ? 'healthy' : 'pending') : 'warning', serversAvailable ? String(serversCount) : t('common.notAvailable')],
    [t('dashboard.agents'), agentsAvailable ? (agentsCount > 0 ? 'healthy' : 'pending') : 'warning', agentsAvailable ? String(agentsCount) : t('common.notAvailable')],
  ];

  return (
    <WidgetPanel title={t('dashboard.infrastructureHealth')} className="health-widget">
      <div className="health-list">
        {rows.map(([label, status, value]) => (
          <div className="health-row" key={label}>
            <span>{label}</span>
            <strong>{value}</strong>
            <StatusBadge status={String(status)} />
          </div>
        ))}
      </div>
    </WidgetPanel>
  );
}

function QuickActionsWidget() {
  const actions = [
    { label: t('dashboard.addServer'), to: '/servers' },
    { label: t('dashboard.registerAgent'), to: '/servers' },
    { label: t('dashboard.createRoutingProfile'), to: '/routing-profiles' },
    { label: t('dashboard.deployConfiguration'), to: '/protocol-settings' },
    { label: t('dashboard.createUserGroup'), to: '/vpn-accounts' },
  ];

  return (
    <WidgetPanel title={t('dashboard.quickActions')} className="quick-actions-widget">
      <Link className="quick-primary-action" to="/vpn-accounts?create=1"><span>＋</span> {t('dashboard.createVpnAccount')}</Link>
      <div className="quick-action-list">
        {actions.map((action) => <Link to={action.to} key={action.label}><span>{action.label}</span><strong>›</strong></Link>)}
      </div>
    </WidgetPanel>
  );
}

function TrafficOverviewWidget({ daily, available }: { daily: DashboardDailyTraffic[]; available: boolean }) {
  const maximum = daily.reduce((value, item) => Math.max(value, item.totalBytes), 0);
  const total = daily.reduce((value, item) => value + item.totalBytes, 0);

  return (
    <WidgetPanel title={t('dashboard.trafficOverview')} subtitle={`(${t('dashboard.last30Days')})`} className="traffic-widget">
      {!available ? (
        <p className="empty-state">{t('common.notAvailable')}</p>
      ) : (
        <div className="traffic-overview-content">
          <div className="traffic-overview-total">
            <strong>{formatBytes(total)}</strong>
            <span>{t('dashboard.total')}</span>
          </div>
          <div className="traffic-overview-bars">
            {daily.map((item, index) => {
              const percentage = maximum > 0 ? (item.totalBytes / maximum) * 100 : 0;
              const showLabel = index === 0 || index === daily.length - 1 || index % 7 === 0;
              return (
                <div className="traffic-overview-column" key={item.date} title={`${formatTrafficDate(item.date)} · ${formatBytes(item.totalBytes)}`}>
                  <div className="traffic-overview-bar-track">
                    <span className="traffic-overview-bar" style={{ height: `${percentage}%` }} />
                  </div>
                  <small>{showLabel ? formatTrafficDate(item.date) : ''}</small>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </WidgetPanel>
  );
}

function ServersSummaryWidget({ servers }: { servers: Array<{ id: string; name: string; region: string; online: boolean; load: string; traffic: string; status: string }> }) {
  return (
    <WidgetPanel title={t('servers.title')} className="servers-summary-widget dashboard-table-widget">
      {servers.length === 0 ? (
        <p className="empty-state">{t('servers.emptyTitle')}</p>
      ) : (
        <div className="dashboard-table servers-summary-table">
          <div className="dashboard-table-row dashboard-table-head">
            <span>{t('servers.name')}</span><span>{t('servers.region')}</span><span>{t('dashboard.online')}</span><span>{t('servers.load')}</span><span>{t('servers.traffic24h')}</span><span>{t('servers.status')}</span>
          </div>
          {servers.map((server) => {
            const countryCode = getRegionCountryCode(server.region);

            return (
              <div className="dashboard-table-row" key={server.id}>
                <strong title={server.name}>{server.name}</strong>
                <span className="server-region-cell">
                  {countryCode && (
                    <span className={`server-country-flag server-country-${countryCode.toLowerCase()}`} aria-label={t('dashboard.countryCode', { code: countryCode })} />
                  )}
                  <span className="server-region-name">{server.region}</span>
                </span>
                <span className={server.online ? 'server-online-dot' : 'server-offline-dot'} />
                <span>{server.load}</span>
                <span>{server.traffic}</span>
                <StatusBadge status={server.status} />
              </div>
            );
          })}
        </div>
      )}
      <Link className="widget-link" to="/servers">{t('dashboard.allServers')} →</Link>
    </WidgetPanel>
  );
}

function RecentDeploymentsWidget({ deployments, available }: { deployments: DashboardRecentDeployment[]; available: boolean }) {
  return (
    <WidgetPanel title={t('dashboard.recentDeployments')} className="deployments-widget dashboard-table-widget">
      {!available ? (
        <p className="empty-state">{t('common.notAvailable')}</p>
      ) : deployments.length === 0 ? (
        <p className="empty-state">0</p>
      ) : (
        <div className="dashboard-table deployments-table">
          <div className="dashboard-table-row dashboard-table-head">
            <span>{t('dashboard.configurationColumn')}</span>
            <span>{t('dashboard.target')}</span>
            <span>{t('agents.action')}</span>
            <span>{t('servers.status')}</span>
            <span>{t('dashboard.time')}</span>
          </div>
          {deployments.map((deployment) => (
            <div className="dashboard-table-row" key={deployment.id}>
              <strong title={deployment.configVersionId}>v{deployment.configVersion}</strong>
              <span title={deployment.serverName}>{deployment.serverName}</span>
              <span>{deployment.action}</span>
              <StatusBadge status={deployment.status} />
              <span title={formatDate(deployment.completedAt ?? deployment.createdAt)}>{formatDate(deployment.completedAt ?? deployment.createdAt)}</span>
            </div>
          ))}
        </div>
      )}
    </WidgetPanel>
  );
}

function RecentAuditEventsWidget({ events, available }: { events: DashboardRecentAuditEvent[]; available: boolean }) {
  return (
    <WidgetPanel title={t('dashboard.recentAuditEvents')} className="audit-widget">
      {!available ? (
        <p className="empty-state">{t('common.notAvailable')}</p>
      ) : events.length === 0 ? (
        <p className="empty-state">0</p>
      ) : (
        <div className="audit-list">
          {events.map((event) => (
            <div className="audit-row" key={event.id}>
              <span className="audit-avatar" aria-hidden="true">{getActorInitial(event.actor)}</span>
              <div>
                <strong title={event.actor}>{event.actor}</strong>
                <p title={`${event.action} · ${formatAuditResource(event)}`}>{event.action} · {formatAuditResource(event)}</p>
              </div>
              <small title={formatDate(event.createdAt)}>{formatDate(event.createdAt)}</small>
              <span className="audit-area-badge">{translateStatus(event.result)}</span>
            </div>
          ))}
        </div>
      )}
    </WidgetPanel>
  );
}

export function DashboardPage() {
  const managerHealthQuery = useQuery({
    queryKey: ['manager-health'],
    queryFn: getManagerHealth,
    refetchInterval: 10_000,
  });

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
    refetchInterval: 10_000,
  });

  const agentsQuery = useQuery({
    queryKey: ['agents'],
    queryFn: getAgents,
    refetchInterval: 10_000,
  });

  const dashboardActivityQuery = useQuery({
    queryKey: ['dashboard', 'activity'],
    queryFn: getDashboardActivity,
    refetchInterval: 10_000,
  });

  const dashboardTrafficQuery = useQuery({
    queryKey: ['dashboard', 'traffic'],
    queryFn: getDashboardTraffic,
    refetchInterval: 60_000,
  });

  const vpnAccountsCountQuery = useQuery({
    queryKey: ['vpn-accounts', 'dashboard-count', 'all'],
    queryFn: () => getPagedVpnAccounts({ page: 1, pageSize: 1 }),
    refetchInterval: 10_000,
  });

  const activeVpnAccountsCountQuery = useQuery({
    queryKey: ['vpn-accounts', 'dashboard-count', 'active'],
    queryFn: () => getPagedVpnAccounts({ status: 'active', page: 1, pageSize: 1 }),
    refetchInterval: 10_000,
  });

  useQuery({
    queryKey: ['me'],
    queryFn: getMe,
    retry: false,
  });

  const managerHealthy = managerHealthQuery.isSuccess;
  const servers = serversQuery.data?.items ?? [];
  const agents = agentsQuery.data?.items ?? [];

  const serversCount = servers.length;
  const activeServersCount = servers.filter((server) => server.status === 'active').length;
  const connectedServersCount = servers.filter((server) => server.agent?.status === 'online').length;
  const disconnectedServersCount = Math.max(serversCount - connectedServersCount, 0);

  const agentsCount = agents.length;
  const onlineAgentsCount = agents.filter((agent) => agent.status === 'online').length;
  const offlineAgentsCount = Math.max(agentsCount - onlineAgentsCount, 0);

  const activeVpnUsers = activeVpnAccountsCountQuery.data?.total ?? 0;
  const vpnAccountsCount = vpnAccountsCountQuery.data?.total ?? 0;
  const trafficAvailable = dashboardTrafficQuery.isSuccess;
  const monthlyTraffic = dashboardTrafficQuery.data?.monthly;
  const serverTrafficById = new Map((dashboardTrafficQuery.data?.servers ?? []).map((item) => [item.serverId, item]));

  const displayServers = servers.slice(0, 5).map((server, index) => ({
    id: server.id,
    name: server.name || `server-${index + 1}`,
    region: server.location || server.provider || t('common.notAvailable'),
    online: server.agent?.status === 'online',
    load: t('common.notAvailable'),
    traffic: trafficAvailable ? formatBytes(serverTrafficById.get(server.id)?.totalBytes ?? 0) : t('common.notAvailable'),
    status: server.status || 'unknown',
  }));

  return (
    <section className="page dashboard-page dashboard-reference-page dashboard-fidelity-page">
      <div className="dashboard-reference-grid">
        <GettingStartedWidget />
        <KpiWidget
          title={t('dashboard.activeServers')}
          value={`${activeServersCount} / ${serversCount}`}
          meta={`${t('dashboard.online')}: ${connectedServersCount} · ${t('dashboard.offline')}: ${disconnectedServersCount}`}
          tone="blue"
          icon="▤"
        />
        <KpiWidget
          title={t('dashboard.onlineAgents')}
          value={`${onlineAgentsCount} / ${agentsCount}`}
          meta={`${t('dashboard.connected')}: ${onlineAgentsCount} · ${t('dashboard.noConnection')}: ${offlineAgentsCount}`}
          tone="cyan"
          icon="⌘"
        />
        <KpiWidget
          title={t('dashboard.activeVpnUsers')}
          value={String(activeVpnUsers)}
          meta={t('vpnAccounts.accountCount', { count: vpnAccountsCount })}
          tone="purple"
          icon="◉"
        />
        <KpiWidget
          title={t('dashboard.monthlyTraffic')}
          value={trafficAvailable && monthlyTraffic ? formatBytes(monthlyTraffic.totalBytes) : t('common.notAvailable')}
          meta={trafficAvailable && monthlyTraffic
            ? `${t('dashboard.inboundTraffic')}: ${formatBytes(monthlyTraffic.rxBytes)} · ${t('dashboard.outboundTraffic')}: ${formatBytes(monthlyTraffic.txBytes)}`
            : t('common.notAvailable')}
          tone="amber"
          icon="☁"
        />

        <InfrastructureHealthWidget
          managerHealthy={managerHealthy}
          serversCount={serversCount}
          agentsCount={agentsCount}
          serversAvailable={serversQuery.isSuccess}
          agentsAvailable={agentsQuery.isSuccess}
        />
        <UnavailableWidget title={t('dashboard.nodeDistribution')} className="node-widget" />
        <TrafficOverviewWidget daily={dashboardTrafficQuery.data?.daily ?? []} available={trafficAvailable} />
        <QuickActionsWidget />
        <ServersSummaryWidget servers={displayServers} />
        <RecentDeploymentsWidget
          deployments={dashboardActivityQuery.data?.recentDeployments ?? []}
          available={dashboardActivityQuery.isSuccess}
        />
        <UnavailableWidget title={t('dashboard.trafficTypes')} subtitle={`(${t('dashboard.month')})`} className="traffic-types-widget" />
        <RecentAuditEventsWidget
          events={dashboardActivityQuery.data?.recentAuditEvents ?? []}
          available={dashboardActivityQuery.isSuccess}
        />
      </div>
      <div className="dashboard-server-time">{t('dashboard.serverTime', { time: formatDate(managerHealthQuery.data?.timestamp) })}</div>
    </section>
  );
}
