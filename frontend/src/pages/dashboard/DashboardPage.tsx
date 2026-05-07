import { useQuery } from '@tanstack/react-query';
import { getManagerHealth } from '../../entities/health/api/healthApi';

export function DashboardPage() {
  const managerHealthQuery = useQuery({
    queryKey: ['manager-health'],
    queryFn: getManagerHealth,
    refetchInterval: 10_000,
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
          <div className="card-value">0</div>
          <div className="card-meta">Server registry is not implemented yet.</div>
        </div>

        <div className="card">
          <div className="card-title">Agents</div>
          <div className="card-value">0</div>
          <div className="card-meta">Agent heartbeat is not implemented yet.</div>
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
              {managerHealthQuery.isSuccess ? 'online' : managerHealthQuery.isError ? 'offline' : 'checking'}
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
