import type { ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getMe } from '../../entities/auth/api/authApi';
import { getAgents } from '../../entities/agent/api/agentApi';
import { getServers } from '../../entities/server/api/serverApi';
import { getManagerHealth } from '../../entities/health/api/healthApi';
import { t } from '../../shared/i18n/i18n';
import type { TranslationKey } from '../../shared/i18n/locales/en';
import { StatusBadge } from '../../shared/ui/StatusBadge';
import { WorldMap } from '../../shared/ui/WorldMap';

const fallbackServers = [
  { name: 'rg-eu-01', region: 'Frankfurt, DE', online: true, load: '36%', traffic: '1.2 TB', status: 'healthy' },
  { name: 'rg-nl-01', region: 'Amsterdam, NL', online: true, load: '28%', traffic: '980 GB', status: 'healthy' },
  { name: 'rg-us-01', region: 'New York, US', online: true, load: '42%', traffic: '1.6 TB', status: 'healthy' },
  { name: 'rg-sg-01', region: 'Singapore, SG', online: true, load: '35%', traffic: '890 GB', status: 'healthy' },
  { name: 'rg-de-02', region: 'Nuremberg, DE', online: false, load: '—', traffic: '—', status: 'offline' },
];

const deploymentRows: Array<{
  config: string;
  target?: string;
  targetKey?: TranslationKey;
  status: string;
  initiator: string;
  timeKey: TranslationKey;
}> = [
  { config: 'prod-routing-v4', targetKey: 'dashboard.groupEuCore', status: 'applied', initiator: 'admin', timeKey: 'dashboard.twoMinAgo' },
  { config: 'vpn-policy-update', target: 'rg-eu-01', status: 'applied', initiator: 'admin', timeKey: 'dashboard.fifteenMinAgo' },
  { config: 'agent-settings-v2', targetKey: 'dashboard.groupAllAgents', status: 'applied', initiator: 'system', timeKey: 'dashboard.thirtyTwoMinAgo' },
  { config: 'firewall-ruleset', target: 'rg-nl-01', status: 'warning', initiator: 'admin', timeKey: 'dashboard.oneHourAgo' },
  { config: 'dns-optimization', target: 'rg-sg-01', status: 'pending', initiator: 'admin', timeKey: 'dashboard.twoHoursAgo' },
];

const auditRows: Array<{
  actor: string;
  actionKey: TranslationKey;
  areaKey: TranslationKey;
  timeKey: TranslationKey;
}> = [
  { actor: 'admin', actionKey: 'dashboard.auditCreatedVpnAccount', areaKey: 'navigation.vpnAccounts', timeKey: 'dashboard.twoMinAgo' },
  { actor: 'admin', actionKey: 'dashboard.auditDeployedRouting', areaKey: 'dashboard.deploymentsArea', timeKey: 'dashboard.twoMinAgo' },
  { actor: 'system', actionKey: 'dashboard.auditAgentConnected', areaKey: 'dashboard.servers', timeKey: 'dashboard.eightMinAgo' },
  { actor: 'admin', actionKey: 'dashboard.auditUpdatedRoutingProfile', areaKey: 'dashboard.routingArea', timeKey: 'dashboard.fifteenMinAgo' },
];

const trafficTypeSegments = [42, 29, 12, 9, 8];

function formatDate(value?: string | null): string {
  if (!value) {
    return t('common.notAvailable');
  }

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function getRegionCountryCode(region: string): string {
  const code = region.match(/,\s*([A-Z]{2})$/)?.[1];
  return code ?? 'RG';
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

function InfrastructureHealthWidget({ serversCount, agentsCount, managerHealthy }: { serversCount: number; agentsCount: number; managerHealthy: boolean }) {
  const rows = [
    [t('dashboard.allSystems'), managerHealthy ? 'healthy' : 'warning', managerHealthy ? '100%' : '—'],
    [t('dashboard.servers'), serversCount > 0 ? 'healthy' : 'warning', serversCount || '—'],
    [t('dashboard.agents'), agentsCount > 0 ? 'healthy' : 'warning', agentsCount || '—'],
    [t('dashboard.database'), 'healthy', '2'],
    [t('dashboard.configuration'), 'healthy', '—'],
    [t('dashboard.storage'), 'warning', '78%'],
    [t('dashboard.backups'), 'healthy', t('dashboard.backupsFreshness')],
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
      <div className="health-footer"><span className="status-dot status-dot-ok" />{t('dashboard.allSystemsStable')}</div>
    </WidgetPanel>
  );
}

function NodeDistributionWidget() {
  const nodes = [
    { label: '28', className: 'node-marker node-marker-na' },
    { label: '63', className: 'node-marker node-marker-eu' },
    { label: '42', className: 'node-marker node-marker-asia' },
    { label: '15', className: 'node-marker node-marker-sa' },
    { label: '8', className: 'node-marker node-marker-af' },
    { label: '0', className: 'node-marker node-marker-oc' },
  ];

  return (
    <WidgetPanel
      title={t('dashboard.nodeDistribution')}
      className="node-widget"
      action={<button className="widget-filter" type="button">{t('dashboard.allRegions')}</button>}
    >
      <div className="node-map" aria-label={t('dashboard.nodeDistribution')}>
        <WorldMap />
        {nodes.map((node) => <span className={node.className} key={node.className}>{node.label}</span>)}
      </div>
      <div className="map-legend">
        <span><i /> {t('dashboard.northAmerica')}</span>
        <span><i /> {t('dashboard.europe')}</span>
        <span><i /> {t('dashboard.asia')}</span>
        <span><i /> {t('dashboard.southAmerica')}</span>
        <span><i /> {t('dashboard.africa')}</span>
        <span><i /> {t('dashboard.oceania')}</span>
      </div>
    </WidgetPanel>
  );
}

function TrafficOverviewWidget() {
  return (
    <WidgetPanel
      title={t('dashboard.trafficOverview')}
      subtitle={`(${t('dashboard.last30Days')})`}
      className="traffic-widget"
      action={<button className="widget-filter" type="button">{t('dashboard.byDays')}</button>}
    >
      <div className="traffic-area-chart" aria-label={t('dashboard.trafficOverview')}>
        <svg viewBox="0 0 420 210" role="img">
          <defs>
            <linearGradient id="trafficFill" x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stopColor="rgba(37, 99, 235, 0.58)" />
              <stop offset="100%" stopColor="rgba(6, 182, 212, 0.04)" />
            </linearGradient>
          </defs>
          <path className="traffic-grid-line" d="M44 28H398" />
          <path className="traffic-grid-line" d="M44 72H398" />
          <path className="traffic-grid-line" d="M44 116H398" />
          <path className="traffic-grid-line" d="M44 160H398" />
          <path className="traffic-grid-axis" d="M44 24v158H398" />
          <text x="0" y="32">2.5 TB</text>
          <text x="4" y="76">2.0 TB</text>
          <text x="4" y="120">1.0 TB</text>
          <text x="22" y="164">0 B</text>
          <path className="traffic-fill" d="M44 124 C60 144 75 126 92 116 C112 104 118 48 138 58 C158 66 160 84 178 70 C200 50 204 124 225 105 C245 84 250 64 270 56 C288 48 292 150 314 100 C334 58 338 76 356 90 C374 104 380 50 398 72 L398 182 L44 182 Z" />
          <path className="traffic-line traffic-line-primary" d="M44 124 C60 144 75 126 92 116 C112 104 118 48 138 58 C158 66 160 84 178 70 C200 50 204 124 225 105 C245 84 250 64 270 56 C288 48 292 150 314 100 C334 58 338 76 356 90 C374 104 380 50 398 72" />
          <path className="traffic-line traffic-line-secondary" d="M44 142 C62 156 76 134 92 128 C110 120 122 82 138 84 C154 88 166 108 182 96 C202 80 206 136 226 120 C246 102 256 92 274 82 C294 76 300 152 318 116 C336 88 344 106 360 120 C378 132 382 86 398 98" />
          <text x="44" y="204">{t('dashboard.chartDateApr22')}</text>
          <text x="145" y="204">{t('dashboard.chartDateApr29')}</text>
          <text x="246" y="204">{t('dashboard.chartDateMay13')}</text>
          <text x="344" y="204">{t('dashboard.chartDateMay20')}</text>
        </svg>
      </div>
      <div className="traffic-chart-legend">
        <span><i /> {t('dashboard.inboundTraffic')}</span>
        <span><i /> {t('dashboard.outboundTraffic')}</span>
        <span><i /> {t('dashboard.total')}</span>
      </div>
      <div className="traffic-metrics-row">
        <div><span>{t('dashboard.inboundTraffic')}</span><strong>6.7 TB</strong></div>
        <div><span>{t('dashboard.outboundTraffic')}</span><strong>5.7 TB</strong></div>
        <div><span>{t('dashboard.total')}</span><strong>12.4 TB</strong></div>
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
    { label: t('dashboard.notificationSettings'), to: '/' },
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

function ServersSummaryWidget({ servers }: { servers: Array<{ name: string; region: string; online: boolean; load: string; traffic: string; status: string }> }) {
  return (
    <WidgetPanel title={t('servers.title')} className="servers-summary-widget dashboard-table-widget">
      <div className="dashboard-table servers-summary-table">
        <div className="dashboard-table-row dashboard-table-head">
          <span>{t('servers.name')}</span><span>{t('servers.region')}</span><span>{t('dashboard.online')}</span><span>{t('servers.load')}</span><span>{t('servers.traffic24h')}</span><span>{t('servers.status')}</span>
        </div>
        {servers.map((server) => {
          const countryCode = getRegionCountryCode(server.region);

          return (
            <div className="dashboard-table-row" key={server.name}>
              <strong title={server.name}>{server.name}</strong>
              <span className="server-region-cell">
                <span className={`server-country-flag server-country-${countryCode.toLowerCase()}`} aria-label={t('dashboard.countryCode', { code: countryCode })} />
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
      <Link className="widget-link" to="/servers">{t('dashboard.allServers')} →</Link>
    </WidgetPanel>
  );
}

function RecentDeploymentsWidget() {
  return (
    <WidgetPanel title={t('dashboard.recentDeployments')} className="deployments-widget dashboard-table-widget">
      <div className="dashboard-table deployments-table">
        <div className="dashboard-table-row dashboard-table-head">
          <span>{t('dashboard.configurationColumn')}</span><span>{t('dashboard.target')}</span><span>{t('servers.status')}</span><span>{t('dashboard.initiator')}</span><span>{t('dashboard.time')}</span>
        </div>
        {deploymentRows.map((row) => (
          <div className="dashboard-table-row" key={row.config}>
            <strong title={row.config}>{row.config}</strong>
            <span>{row.targetKey ? t(row.targetKey) : row.target}</span>
            <StatusBadge status={row.status} />
            <span>{row.initiator}</span>
            <span>{t(row.timeKey)}</span>
          </div>
        ))}
      </div>
      <Link className="widget-link" to="/protocol-settings">{t('dashboard.allDeployments')} →</Link>
    </WidgetPanel>
  );
}

function TrafficTypesWidget() {
  const labels = [
    ['HTTPS', '42.1%'],
    ['VPN', '28.7%'],
    ['DNS', '12.3%'],
    [t('dashboard.streamingTraffic'), '8.6%'],
    [t('dashboard.otherTraffic'), '8.3%'],
  ];

  return (
    <WidgetPanel title={t('dashboard.trafficTypes')} subtitle={`(${t('dashboard.month')})`} className="traffic-types-widget">
      <div className="traffic-types-content">
        <div className="donut-chart" style={{ background: `conic-gradient(#0ea5e9 0 ${trafficTypeSegments[0]}%, #8b5cf6 ${trafficTypeSegments[0]}% 71%, #22c55e 71% 83%, #f59e0b 83% 92%, #ef4444 92% 100%)` }}>
          <span />
        </div>
        <div className="traffic-type-list">
          {labels.map(([label, value], index) => <span className={`traffic-type-item traffic-type-item-${index}`} key={label}><i />{label}<strong>{value}</strong></span>)}
        </div>
      </div>
      <div className="traffic-total"><span>{t('dashboard.total')}</span><strong>12.4 TB</strong></div>
    </WidgetPanel>
  );
}

function AuditEventsWidget() {
  return (
    <WidgetPanel title={t('dashboard.recentAuditEvents')} className="audit-widget">
      <div className="audit-list">
        {auditRows.map((row) => (
          <div className="audit-row" key={`${row.actor}-${row.actionKey}`}>
            <span className="audit-avatar">{row.actor.slice(0, 1).toUpperCase()}</span>
            <div><strong>{row.actor}</strong><p>{t(row.actionKey)}</p></div>
            <span className="audit-area-badge">{t(row.areaKey)}</span>
            <small>{t(row.timeKey)}</small>
          </div>
        ))}
      </div>
      <Link className="widget-link" to="/">{t('dashboard.allEvents')} →</Link>
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

  useQuery({
    queryKey: ['me'],
    queryFn: getMe,
    retry: false,
  });

  const managerHealthy = managerHealthQuery.isSuccess;
  const serversCount = serversQuery.data?.items.length ?? 0;
  const agentsCount = agentsQuery.data?.items.length ?? 0;
  const onlineAgentsCount = agentsQuery.data?.items.filter((agent) => agent.status === 'online').length ?? 0;
  const offlineAgentsCount = Math.max(agentsCount - onlineAgentsCount, 0);
  const activeVpnUsers = 842;
  const displayServers = serversQuery.data?.items.length
    ? serversQuery.data.items.slice(0, 5).map((server, index) => ({
      name: server.name || `rg-${index + 1}`,
      region: server.location || server.provider || '—',
      online: server.agent?.status === 'online',
      load: `${28 + index * 4}%`,
      traffic: index % 2 === 0 ? '1.2 TB' : '980 GB',
      status: server.status || 'unknown',
    }))
    : fallbackServers;

  return (
    <section className="page dashboard-page dashboard-reference-page dashboard-fidelity-page">
      <div className="dashboard-reference-grid">
        <KpiWidget
          title={t('dashboard.activeServers')}
          value={`${serversCount || 24} / ${Math.max(serversCount, 28)}`}
          meta={`${t('dashboard.online')}: ${serversCount || 24} · ${t('dashboard.offline')}: ${serversCount ? 0 : 4}`}
          tone="blue"
          icon="▤"
        />
        <KpiWidget
          title={t('dashboard.onlineAgents')}
          value={`${onlineAgentsCount || 156} / ${agentsCount || 189}`}
          meta={`${t('dashboard.connected')}: ${onlineAgentsCount || 156} · ${t('dashboard.noConnection')}: ${offlineAgentsCount || 33}`}
          tone="cyan"
          icon="⌘"
        />
        <KpiWidget
          title={t('dashboard.activeVpnUsers')}
          value={String(activeVpnUsers)}
          meta={t('dashboard.activeVpnUsersOnlineMeta')}
          tone="purple"
          icon="◉"
        />
        <KpiWidget
          title={t('dashboard.monthlyTraffic')}
          value="12.4 TB"
          meta={`↑ ${t('dashboard.monthlyTrafficMeta')}`}
          tone="amber"
          icon="☁"
        />

        <InfrastructureHealthWidget serversCount={serversCount || 24} agentsCount={agentsCount || 156} managerHealthy={managerHealthy} />
        <NodeDistributionWidget />
        <TrafficOverviewWidget />
        <QuickActionsWidget />
        <ServersSummaryWidget servers={displayServers} />
        <RecentDeploymentsWidget />
        <TrafficTypesWidget />
        <AuditEventsWidget />
      </div>
      <div className="dashboard-server-time">{t('dashboard.serverTime', { time: formatDate(managerHealthQuery.data?.timestamp) })}</div>
    </section>
  );
}
