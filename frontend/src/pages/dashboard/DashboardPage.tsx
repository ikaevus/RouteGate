import type { ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getMe } from '../../entities/auth/api/authApi';
import { getAgents } from '../../entities/agent/api/agentApi';
import { getServers } from '../../entities/server/api/serverApi';
import { getManagerHealth } from '../../entities/health/api/healthApi';
import { t } from '../../shared/i18n/i18n';
import { StatusBadge } from '../../shared/ui/StatusBadge';

const fallbackServers = [
  { name: 'rg-eu-01', region: 'Frankfurt, DE', online: true, load: '36%', traffic: '1.2 TB', status: 'healthy' },
  { name: 'rg-nl-01', region: 'Amsterdam, NL', online: true, load: '28%', traffic: '980 GB', status: 'healthy' },
  { name: 'rg-us-01', region: 'New York, US', online: true, load: '42%', traffic: '1.6 TB', status: 'healthy' },
  { name: 'rg-sg-01', region: 'Singapore, SG', online: true, load: '35%', traffic: '890 GB', status: 'healthy' },
  { name: 'rg-de-02', region: 'Nuremberg, DE', online: false, load: '—', traffic: '—', status: 'offline' },
];

const deploymentRows = [
  { config: 'prod-routing-v4', target: 'Group: EU-Core', status: 'applied', initiator: 'admin', time: '2 min ago' },
  { config: 'vpn-policy-update', target: 'rg-eu-01', status: 'applied', initiator: 'admin', time: '15 min ago' },
  { config: 'agent-settings-v2', target: 'Group: All Agents', status: 'applied', initiator: 'system', time: '32 min ago' },
  { config: 'firewall-ruleset', target: 'rg-nl-01', status: 'warning', initiator: 'admin', time: '1 h ago' },
  { config: 'dns-optimization', target: 'rg-sg-01', status: 'pending', initiator: 'admin', time: '2 h ago' },
];

const auditRows = [
  { actor: 'admin', action: 'Created VPN account user@routegate.local', area: 'VPN Accounts', time: '2 min ago' },
  { actor: 'admin', action: 'Deployed prod-routing-v4', area: 'Deployments', time: '2 min ago' },
  { actor: 'system', action: 'Agent rg-eu-02 connected', area: 'Agents', time: '8 min ago' },
  { actor: 'admin', action: 'Updated EU-Core routing profile', area: 'Routing', time: '15 min ago' },
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
    ['All systems', managerHealthy ? 'healthy' : 'warning', managerHealthy ? '100%' : '—'],
    [t('dashboard.servers'), serversCount > 0 ? 'healthy' : 'warning', serversCount || '—'],
    [t('dashboard.agents'), agentsCount > 0 ? 'healthy' : 'warning', agentsCount || '—'],
    ['Database', 'healthy', '2'],
    ['Configuration', 'healthy', '—'],
    ['Storage', 'warning', '78%'],
    ['Backups', 'healthy', '2 h ago'],
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
        <svg className="world-map-svg" viewBox="0 0 720 320" role="img" aria-hidden="true">
          <path className="world-map-line" d="M38 160H682M360 24v272M134 38c-36 76-36 168 0 244M586 38c36 76 36 168 0 244" />
          <path className="world-map-land world-map-land-na" d="M74 111c16-35 54-53 101-54 45-1 82 14 111 42 19 19 18 39-5 50-19 10-45 7-61 24-15 16-13 43-33 55-24 14-60 0-73-25-10-19-1-39-19-55-15-13-33-14-21-37Z" />
          <path className="world-map-land world-map-land-gr" d="M221 42c31-16 72-12 92 8-11 20-49 26-83 15-19-6-21-15-9-23Z" />
          <path className="world-map-land world-map-land-sa" d="M243 188c30 15 51 44 50 77-1 34-24 62-48 72-18-20-25-47-22-78 2-29-12-50 20-71Z" />
          <path className="world-map-land world-map-land-eu" d="M356 89c25-22 63-27 97-14 21 8 27 28 8 40-18 11-42 4-59 16-16 11-42 8-55-6-12-13-6-27 9-36Z" />
          <path className="world-map-land world-map-land-af" d="M412 137c42-5 77 24 82 68 5 42-24 76-57 85-32-21-53-57-45-96 5-25 9-44 20-57Z" />
          <path className="world-map-land world-map-land-asia" d="M469 87c48-36 121-33 169 5 35 28 45 70 17 96-33 31-87 8-121 29-30 18-74-3-84-42-9-36 0-70 19-88Z" />
          <path className="world-map-land world-map-land-jp" d="M628 144c12 12 14 30 5 45-14-9-18-30-5-45Z" />
          <path className="world-map-land world-map-land-oc" d="M579 238c36-14 83-2 103 25-17 26-66 31-101 16-26-12-29-31-2-41Z" />
          <path className="world-map-land world-map-land-nz" d="M673 294c16-4 30 0 39 10-13 11-31 11-39-10Z" />
        </svg>
        {nodes.map((node) => <span className={node.className} key={node.className}>{node.label}</span>)}
      </div>
      <div className="map-legend">
        <span><i /> North America</span>
        <span><i /> Europe</span>
        <span><i /> Asia</span>
        <span><i /> South America</span>
        <span><i /> Africa</span>
        <span><i /> Oceania</span>
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
      action={<button className="widget-filter" type="button">By days</button>}
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
          <text x="44" y="204">22 Apr</text>
          <text x="145" y="204">29 Apr</text>
          <text x="246" y="204">13 May</text>
          <text x="344" y="204">20 May</text>
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
    { label: t('dashboard.registerAgent'), to: '/agents' },
    { label: t('dashboard.createRoutingProfile'), to: '/routing-profiles' },
    { label: t('dashboard.deployConfiguration'), to: '/protocol-settings' },
    { label: t('dashboard.createUserGroup'), to: '/vpn-accounts' },
    { label: t('dashboard.notificationSettings'), to: '/' },
  ];

  return (
    <WidgetPanel title={t('dashboard.quickActions')} className="quick-actions-widget">
      <Link className="quick-primary-action" to="/vpn-accounts"><span>＋</span> {t('dashboard.createVpnAccount')}</Link>
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
              <strong>{server.name}</strong>
              <span className="server-region-cell">
                <span className={`server-country-chip server-country-${countryCode.toLowerCase()}`} aria-label={`Country: ${countryCode}`}>{countryCode}</span>
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
          <span>Configuration</span><span>Target</span><span>Status</span><span>Initiator</span><span>Time</span>
        </div>
        {deploymentRows.map((row) => (
          <div className="dashboard-table-row" key={row.config}>
            <strong>{row.config}</strong>
            <span>{row.target}</span>
            <StatusBadge status={row.status} />
            <span>{row.initiator}</span>
            <span>{row.time}</span>
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
    ['Streaming', '8.6%'],
    ['Other', '8.3%'],
  ];

  return (
    <WidgetPanel title={t('dashboard.trafficTypes')} subtitle="(month)" className="traffic-types-widget">
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
          <div className="audit-row" key={`${row.actor}-${row.action}`}>
            <span className="audit-avatar">{row.actor.slice(0, 1).toUpperCase()}</span>
            <div><strong>{row.actor}</strong><p>{row.action}</p></div>
            <span className="audit-area-badge">{row.area}</span>
            <small>{row.time}</small>
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
          meta={`${t('dashboard.connected')}: ${onlineAgentsCount || 156} · ${t('dashboard.offline')}: ${offlineAgentsCount || 33}`}
          tone="cyan"
          icon="⌘"
        />
        <KpiWidget
          title={t('dashboard.activeVpnUsers')}
          value={String(activeVpnUsers)}
          meta="612 online right now"
          tone="purple"
          icon="◉"
        />
        <KpiWidget
          title={t('dashboard.monthlyTraffic')}
          value="12.4 TB"
          meta="↑ 18% from previous month"
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
      <div className="dashboard-server-time">Server time: {formatDate(managerHealthQuery.data?.timestamp)}</div>
    </section>
  );
}
