import { Link, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { DashboardPage } from '../pages/dashboard/DashboardPage';
import { ServersPage } from '../pages/servers/ServersPage';
import { ServerDetailsPage } from '../pages/servers/ServerDetailsPage';
import { AgentsPage } from '../pages/agents/AgentsPage';
import { ProtocolSettingsPage } from '../pages/protocol-settings/ProtocolSettingsPage';
import { VpnAccountsPage } from '../pages/vpn-accounts/VpnAccountsPage';
import { RoutingProfilesPage } from '../pages/routing-profiles/RoutingProfilesPage';
import { LoginPage } from '../pages/login/LoginPage';
import { PortalPage } from '../pages/portal/PortalPage';

function PortalShell() {
  return (
    <div className="portal-app-shell">
      <header className="portal-topbar">
        <Link className="portal-brand" to="/portal">
          <div className="brand-mark">RG</div>
          <div>
            <div className="brand-title">RouteGate</div>
            <div className="brand-subtitle">User Portal</div>
          </div>
        </Link>

        <nav className="portal-topnav">
          <Link to="/portal">Portal Dashboard</Link>
          <Link to="/">Admin UI</Link>
          <Link to="/login">Login</Link>
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
            <div className="brand-subtitle">Foundation</div>
          </div>
        </div>

        <nav className="nav">
          <Link to="/">Dashboard</Link>
          <Link to="/servers">Servers</Link>
          <Link to="/agents">Agents</Link>
          <Link to="/protocol-settings">Protocol Settings</Link>
          <Link to="/vpn-accounts">VPN Accounts</Link>
          <Link to="/routing-profiles">Routing Profiles</Link>
          <Link to="/portal">User Portal</Link>
          <Link to="/login">Login</Link>
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
