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

const adminNavigationItems = [
  { to: '/', label: t('navigation.overview'), icon: '⌂', end: true },
  { to: '/servers', label: t('navigation.servers'), icon: '▤' },
  { to: '/agents', label: t('navigation.agents'), icon: '◇' },
  { to: '/vpn-accounts', label: t('navigation.vpnAccounts'), icon: '◉' },
  { to: '/protocol-settings', label: t('navigation.configDeploy'), icon: '▦' },
  { to: '/routing-profiles', label: t('navigation.routingProfiles'), icon: '⌘' },
  { to: '/portal', label: t('navigation.userPortal'), icon: '□' },
  { to: '/login', label: t('navigation.login'), icon: '↪' },
];

const secondaryNavigationItems = [
  { label: t('navigation.security'), icon: '盾' },
  { label: t('navigation.licensing'), icon: '◎' },
  { label: t('navigation.appliance'), icon: '▣' },
];

function PortalShell() {
  return (
    <div className="portal-app-shell">
      <header className="portal-topbar">
        <Link className="portal-brand" to="/portal">
          <div className="brand-mark">RG</div>
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
    <div className="app-shell routegate-admin-shell">
      <aside className="sidebar routegate-sidebar">
        <div className="brand routegate-brand">
          <div className="brand-mark routegate-brand-mark">RG</div>
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
              <span className="nav-icon" aria-hidden="true">{item.icon}</span>
              <span>{item.label}</span>
            </NavLink>
          ))}
        </nav>

        <nav className="nav routegate-nav routegate-nav-secondary" aria-label="Secondary navigation">
          {secondaryNavigationItems.map((item) => (
            <span className="nav-link nav-link-muted" key={item.label}>
              <span className="nav-icon" aria-hidden="true">{item.icon}</span>
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
          <div className="license-progress-track"><span /></div>
        </div>

        <button className="sidebar-control" type="button">◐ Dark theme</button>
        <button className="sidebar-control" type="button">← {t('navigation.collapse')}</button>
      </aside>

      <div className="admin-workspace">
        <header className="admin-topbar">
          <button className="topbar-menu-button" type="button" aria-label="Toggle sidebar">☰</button>
          <label className="topbar-search">
            <span aria-hidden="true">⌕</span>
            <input placeholder={t('topbar.searchPlaceholder')} />
            <kbd>{t('topbar.shortcut')}</kbd>
          </label>
          <div className="topbar-actions">
            <button className="topbar-icon-button topbar-notification" type="button" aria-label={t('topbar.notifications')}>◌</button>
            <button className="topbar-icon-button" type="button" aria-label={t('topbar.help')}>?</button>
            <button className="topbar-icon-button" type="button" aria-label={t('topbar.settings')}>⚙</button>
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
