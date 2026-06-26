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
  { to: '/', label: t('navigation.overview'), end: true },
  { to: '/servers', label: t('navigation.servers') },
  { to: '/agents', label: t('navigation.agents') },
  { to: '/protocol-settings', label: t('navigation.protocolSettings') },
  { to: '/vpn-accounts', label: t('navigation.vpnAccounts') },
  { to: '/routing-profiles', label: t('navigation.routingProfiles') },
  { to: '/portal', label: t('navigation.userPortal') },
  { to: '/login', label: t('navigation.login') },
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
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">RG</div>
          <div>
            <div className="brand-title">RouteGate</div>
            <div className="brand-subtitle">{t('app.adminSubtitle')}</div>
          </div>
        </div>

        <nav className="nav">
          {adminNavigationItems.map((item) => (
            <NavLink
              className={({ isActive }) => (isActive ? 'nav-link nav-link-active' : 'nav-link')}
              end={item.end}
              key={item.to}
              to={item.to}
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>

      <main className="main">
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
