import { useQuery } from '@tanstack/react-query';
import { getMe } from '../../entities/auth/api/authApi';
import { getAgents } from '../../entities/agent/api/agentApi';
import { getServers } from '../../entities/server/api/serverApi';
import { getManagerHealth } from '../../entities/health/api/healthApi';

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

  const meQuery = useQuery({
    queryKey: ['me'],
    queryFn: getMe,
    retry: false,
  });

  const managerStatusLabel = managerHealthQuery.isSuccess
    ? managerHealthQuery.data.status.toUpperCase()
    : managerHealthQuery.isError
      ? 'OFFLINE'
      : 'CHECKING';

  const managerServiceLabel = managerHealthQuery.isSuccess
    ? managerHealthQuery.data.service
    : 'Waiting for /api/admin/health';

  const managerTimestamp = managerHealthQuery.isSuccess
    ? managerHealthQuery.data.timestamp
    : '—';

  const serversCount = serversQuery.data?.items.length ?? 0;
  const agentsCount = agentsQuery.data?.items.length ?? 0;
  const onlineAgentsCount =
    agentsQuery.data?.items.filter((agent) => agent.status === 'online').length ?? 0;

  return (
    <section className="page">
      <div className="page-header">
        <div>
          <h1>Dashboard</h1>
          <p>RouteGate Foundation control plane overview.</p>
        </div>

        <div className="status-pill">
          <span
            className={
              managerHealthQuery.isSuccess ? 'status-dot status-dot-ok' : 'status-dot status-dot-warn'
            }
          />
          {managerHealthQuery.isSuccess ? 'Manager online' : 'Checking manager'}
        </div>
      </div>

      <div className="card-grid">
        <div className="card">
          <div className="card-title">Manager API</div>
          <div className="card-value">{managerStatusLabel}</div>
          <div className="card-meta">{managerServiceLabel}</div>
        </div>

        <div className="card">
          <div className="card-title">Servers</div>
          <div className="card-value">
            {serversQuery.isLoading ? '...' : serversCount}
          </div>
          <div className="card-meta">
            {serversQuery.isError
              ? 'Failed to load servers.'
              : 'Registered server records from Manager API.'}
          </div>
        </div>

        <div className="card">
          <div className="card-title">Agents</div>
          <div className="card-value">
            {agentsQuery.isLoading ? '...' : agentsCount}
          </div>
          <div className="card-meta">
            {agentsQuery.isError
              ? 'Failed to load agents.'
              : `${onlineAgentsCount} online agent(s).`}
          </div>
        </div>

        <div className="card">
          <div className="card-title">Current user</div>
          <div className="card-value card-value-small">
            {meQuery.isSuccess ? meQuery.data.user.displayName : 'Guest'}
          </div>
          <div className="card-meta">
            {meQuery.isSuccess
              ? meQuery.data.user.email
              : 'Open Login and sign in with dev credentials.'}
          </div>
        </div>
      </div>

      <div className="panel">
        <div className="panel-title">Foundation status</div>

        <div className="status-list">
          <div className="status-row">
            <span>Frontend</span>
            <strong>online</strong>
          </div>

          <div className="status-row">
            <span>Manager API</span>
            <strong>
              {managerHealthQuery.isSuccess
                ? 'online'
                : managerHealthQuery.isError
                  ? 'offline'
                  : 'checking'}
            </strong>
          </div>

          <div className="status-row">
            <span>Server registry</span>
            <strong>
              {serversQuery.isSuccess
                ? `${serversCount} registered`
                : serversQuery.isError
                  ? 'error'
                  : 'checking'}
            </strong>
          </div>

          <div className="status-row">
            <span>Agent registry</span>
            <strong>
              {agentsQuery.isSuccess
                ? `${agentsCount} registered`
                : agentsQuery.isError
                  ? 'error'
                  : 'checking'}
            </strong>
          </div>

          <div className="status-row">
            <span>Last health timestamp</span>
            <strong>{managerTimestamp}</strong>
          </div>
        </div>
      </div>
    </section>
  );
}
