import type { ReactNode } from 'react';
import { Link, Navigate, NavLink, Route, Routes, useLocation } from 'react-router-dom';
import { DashboardPage } from '../pages/dashboard/DashboardPage';
import { ServersPage } from '../pages/servers/ServersPage';
import { ServerDetailsPage } from '../pages/servers/ServerDetailsPage';
import { AgentsPage } from '../pages/agents/AgentsPage';
import { ProtocolSettingsPage } from '../pages/protocol-settings/ProtocolSettingsPage';
import { VpnAccountsPage } from '../pages/vpn-accounts/VpnAccountsPage';
import { RoutingProfilesPage } from '../pages/routing-profiles/RoutingProfilesPage';
import { LoginPage } from '../pages/login/LoginPage';
import { PortalPage } from '../pages/portal/PortalPage';
import { t } from '../shared/i18n/i18n';

type IconName =
  | 'overview'
  | 'servers'
  | 'agents'
  | 'accounts'
  | 'deploy'
  | 'routing'
  | 'portal'
  | 'login'
  | 'security'
  | 'licensing'
  | 'appliance'
  | 'search'
  | 'bell'
  | 'help'
  | 'settings'
  | 'menu'
  | 'moon'
  | 'collapse';

function Icon({ name }: { name: IconName }) {
  const paths: Record<IconName, ReactNode> = {
    overview: <><path d="M3.5 11.5 12 4l8.5 7.5" /><path d="M5.5 10.5V20h13v-9.5" /><path d="M9.5 20v-5.5h5V20" /></>,
    servers: <><rect x="4" y="5" width="16" height="5" rx="1.4" /><rect x="4" y="14" width="16" height="5" rx="1.4" /><path d="M7 7.5h.01M7 16.5h.01M11 7.5h6M11 16.5h6" /></>,
    agents: <><circle cx="12" cy="7" r="3" /><path d="M5.5 20a6.5 6.5 0 0 1 13 0" /><path d="M4 12.5h3M17 12.5h3" /></>,
    accounts: <><circle cx="8.5" cy="10" r="3" /><circle cx="15.5" cy="8" r="2.5" /><path d="M3.5 20a5 5 0 0 1 10 0" /><path d="M13.5 14.5A4.5 4.5 0 0 1 20.5 18" /></>,
    deploy: <><path d="M6 5h12v14H6z" /><path d="M9 8h6M9 12h6M9 16h4" /></>,
    routing: <><circle cx="5" cy="12" r="2" /><circle cx="19" cy="6" r="2" /><circle cx="19" cy="18" r="2" /><path d="M7 12h4c3 0 4-6 6-6M7 12h4c3 0 4 6 6 6" /></>,
    portal: <><rect x="5" y="6" width="14" height="12" rx="2" /><path d="M9 10h6M9 14h4" /></>,
    login: <><path d="M10 17l5-5-5-5" /><path d="M15 12H3" /><path d="M13 4h5a3 3 0 0 1 3 3v10a3 3 0 0 1-3 3h-5" /></>,
    security: <><path d="M12 3 5 6v5c0 4.5 2.8 8.5 7 10 4.2-1.5 7-5.5 7-10V6l-7-3Z" /><path d="m9 12 2 2 4-5" /></>,
    licensing: <><circle cx="12" cy="12" r="7" /><path d="M9.5 12.5 11 14l3.5-4" /></>,
    appliance: <><rect x="4" y="6" width="16" height="12" rx="2" /><path d="M8 10h8M8 14h3M15 14h1" /></>,
    search: <><circle cx="10.5" cy="10.5" r="5.5" /><path d="m15 15 4 4" /></>,
    bell: <><path d="M18 16H6c1.2-1.3 1.8-2.9 1.8-4.7V9a4.2 4.2 0 0 1 8.4 0v2.3c0 1.8.6 3.4 1.8 4.7Z" /><path d="M10 19a2 2 0 0 0 4 0" /></>,
    help: <><circle cx="12" cy="12" r="8" /><path d="M10 9a2.2 2.2 0 0 1 4.2 1c0 1.8-2.2 1.9-2.2 3.5" /><path d="M12 17h.01" /></>,
    settings: <><circle cx="12" cy="12" r="3" /><path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M18.4 5.6l-2.1 2.1M7.7 16.3l-2.1 2.1" /></>,
    menu: <><path d="M5 7h14M5 12h14M5 17h14" /></>,
    moon: <><path d="M19 14.5A7.5 7.5 0 0 1 9.5 5 7.6 7.6 0 1 0 19 14.5Z" /></>,
    collapse: <><path d="M15 6 9 12l6 6" /><path d="M9 12h12" /></>,
  };

  return (
    <svg aria-hidden="true" className="rg-icon" viewBox="0 0 24 24">
      {paths[name]}
    </svg>
  );
}

function BrandShield() {
  return (
    <div className="brand-shield" aria-hidden="true">
      <svg viewBox="0 0 32 32">
        <path className="brand-shield-outer" d="M16 2.8 5.5 6.9v7.8c0 6.9 4.2 12.4 10.5 14.5 6.3-2.1 10.5-7.6 10.5-14.5V6.9L16 2.8Z" />
        <path className="brand-shield-inner" d="M16 7.2 10 9.5v5.2c0 4.1 2.3 7.2 6 8.8 3.7-1.6 6-4.7 6-8.8V9.5L16 7.2Z" />
        <path className="brand-shield-gate" d="M13 20v-6.2c0-1.6 1.2-2.8 3-2.8s3 1.2 3 2.8V20" />
      </svg>
    </div>
  );
}

const adminNavigationItems = [
  { to: '/', label: t('navigation.overview'), icon: 'overview' as const, end: true },
  { to: '/servers', label: t('navigation.servers'), icon: 'servers' as const },
  { to: '/agents', label: t('navigation.agents'), icon: 'agents' as const },
  { to: '/vpn-accounts', label: t('navigation.vpnAccounts'), icon: 'accounts' as const },
  { to: '/protocol-settings', label: t('navigation.configDeploy'), icon: 'deploy' as const },
  { to: '/routing-profiles', label: t('navigation.routingProfiles'), icon: 'routing' as const },
  { to: '/portal', label: t('navigation.userPortal'), icon: 'portal' as const },
  { to: '/login', label: t('navigation.login'), icon: 'login' as const },
];

const secondaryNavigationItems = [
  { label: t('navigation.security'), icon: 'security' as const },
  { label: t('navigation.licensing'), icon: 'licensing' as const },
  { label: t('navigation.appliance'), icon: 'appliance' as const },
];

function PortalShell() {
  return (
    <div className="portal-app-shell">
      <header className="portal-topbar">
        <Link className="portal-brand" to="/portal">
          <BrandShield />
          <div>
            <div className="brand-title">RouteGate</div>
            <div className="brand-subtitle">{t('app.portalSubtitle')}</div>
          </div>
        </Link>

        <nav className="portal-topnav">
          <Link to="/portal">{t('navigation.userPortal')}</Link>
          <Link to="/">{t('navigation.adminUi')}</Link>
          <Link to="/login">{t('navigation.login')}</Link>
        </nav>
      </header>

      <main className="portal-main">
        <Routes>
          <Route path="/portal" element={<PortalPage />} />
          <Route path="/portal/profiles/:profileId" element={<PortalPage />} />
          <Route path="*" element={<Navigate to="/portal" replace />} />
        </Routes>
      </main>
    </div>
  );
}

function AdminShell() {
  return (
    <div className="app-shell routegate-admin-shell routegate-reference-shell">
      <aside className="sidebar routegate-sidebar">
        <div className="brand routegate-brand">
          <BrandShield />
          <div>
            <div className="brand-title">RouteGate</div>
            <div className="brand-subtitle">{t('app.adminSubtitle')}</div>
          </div>
        </div>

        <nav className="nav routegate-nav">
          {adminNavigationItems.map((item) => (
            <NavLink
              className={({ isActive }) => (isActive ? 'nav-link nav-link-active' : 'nav-link')}
              end={item.end}
              key={item.to}
              to={item.to}
            >
              <span className="nav-icon" aria-hidden="true"><Icon name={item.icon} /></span>
              <span>{item.label}</span>
            </NavLink>
          ))}
        </nav>

        <nav className="nav routegate-nav routegate-nav-secondary" aria-label="Secondary navigation">
          {secondaryNavigationItems.map((item) => (
            <span className="nav-link nav-link-muted" key={item.label}>
              <span className="nav-icon" aria-hidden="true"><Icon name={item.icon} /></span>
              <span>{item.label}</span>
            </span>
          ))}
        </nav>

        <div className="sidebar-spacer" />

        <div className="sidebar-license-card">
          <div className="sidebar-license-header">
            <span>{t('dashboard.license')}</span>
            <strong>{t('dashboard.licenseActive')}</strong>
          </div>
          <p>{t('dashboard.licenseExpires')}</p>
          <p>Nodes: 24 / 100</p>
          <div className="license-progress-track"><span /></div>
        </div>

        <button className="sidebar-control sidebar-theme-control" type="button">
          <Icon name="moon" />
          <span>Dark theme</span>
          <i />
        </button>
        <button className="sidebar-control" type="button"><Icon name="collapse" /> {t('navigation.collapse')}</button>
      </aside>

      <div className="admin-workspace">
        <header className="admin-topbar">
          <button className="topbar-menu-button" type="button" aria-label="Toggle sidebar"><Icon name="menu" /></button>
          <label className="topbar-search">
            <Icon name="search" />
            <input placeholder={t('topbar.searchPlaceholder')} />
            <kbd>{t('topbar.shortcut')}</kbd>
          </label>
          <div className="topbar-actions">
            <button className="topbar-icon-button topbar-notification" type="button" aria-label={t('topbar.notifications')}><Icon name="bell" /></button>
            <button className="topbar-icon-button" type="button" aria-label={t('topbar.help')}><Icon name="help" /></button>
            <button className="topbar-icon-button" type="button" aria-label={t('topbar.settings')}><Icon name="settings" /></button>
            <div className="admin-profile-chip">
              <span>AD</span>
              <div>
                <strong>admin</strong>
                <small>{t('topbar.adminRole')}</small>
              </div>
            </div>
          </div>
        </header>

        <main className="main admin-main">
          <Routes>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/servers" element={<ServersPage />} />
            <Route path="/servers/:serverId" element={<ServerDetailsPage />} />
            <Route path="/agents" element={<AgentsPage />} />
            <Route path="/protocol-settings" element={<ProtocolSettingsPage />} />
            <Route path="/protocol-settings/:serverId" element={<ProtocolSettingsPage />} />
            <Route path="/vpn-accounts" element={<VpnAccountsPage />} />
            <Route path="/vpn-accounts/:accountId" element={<VpnAccountsPage />} />
            <Route path="/routing-profiles" element={<RoutingProfilesPage />} />
            <Route path="/routing-profiles/:profileId" element={<RoutingProfilesPage />} />
            <Route path="/login" element={<LoginPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>

        <footer className="admin-statusbar">
          <span>{t('app.footerProduct')}</span>
          <span>{t('app.version')}</span>
          <strong><span className="status-dot status-dot-ok" /> {t('dashboard.systemsOperational')}</strong>
          <span className="admin-statusbar-time">Server time: 20.05.2026 14:32:11 (UTC+3)</span>
        </footer>
      </div>
    </div>
  );
}

export function App() {
  const location = useLocation();

  if (location.pathname.startsWith('/portal')) {
    return <PortalShell />;
  }

  return <AdminShell />;
}
