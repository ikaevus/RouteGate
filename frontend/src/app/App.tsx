import { Link, Navigate, Route, Routes } from 'react-router-dom';
import { DashboardPage } from '../pages/dashboard/DashboardPage';
import { ServersPage } from '../pages/servers/ServersPage';
import { ServerDetailsPage } from '../pages/servers/ServerDetailsPage';
import { AgentsPage } from '../pages/agents/AgentsPage';
import { LoginPage } from '../pages/login/LoginPage';

export function App() {
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
          <Link to="/login">Login</Link>
        </nav>
      </aside>

      <main className="main">
        <Routes>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/servers" element={<ServersPage />} />
          <Route path="/servers/:serverId" element={<ServerDetailsPage />} />
          <Route path="/agents" element={<AgentsPage />} />
          <Route path="/login" element={<LoginPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  );
}
