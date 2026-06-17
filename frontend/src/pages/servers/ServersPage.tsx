import { useQuery } from '@tanstack/react-query';
import { getServers, type Server } from '../../entities/server/api/serverApi';

function formatDate(value?: string | null): string {
  if (!value) {
    return '—';
  }

  return new Date(value).toLocaleString();
}

function formatValue(value?: string | null): string {
  return value && value.trim() !== '' ? value : '—';
}

function StatusBadge({ status }: { status?: string | null }) {
  const normalizedStatus = status && status.trim() !== '' ? status : 'unknown';

  return <span className={`badge badge-${normalizedStatus}`}>{normalizedStatus}</span>;
}

function ServerRow({ server }: { server: Server }) {
  return (
    <div className="table-row servers-table-row">
      <div>
        <strong>{formatValue(server.name)}</strong>
        <span>{formatValue(server.description)}</span>
      </div>
      <div>{formatValue(server.provider)}</div>
      <div>{formatValue(server.location)}</div>
      <div>{formatValue(server.publicIp)}</div>
      <div>
        <StatusBadge status={server.status} />
      </div>
      <div>
        {server.agent ? (
          <StatusBadge status={server.agent.status} />
        ) : (
          <span className="muted-text">—</span>
        )}
      </div>
      <div>{formatValue(server.agent?.agentVersion)}</div>
      <div>{formatDate(server.agent?.lastSeenAt)}</div>
      <div>{formatDate(server.createdAt)}</div>
    </div>
  );
}

export function ServersPage() {
  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
  });

  const servers = serversQuery.data?.items ?? [];

  return (
    <section className="page servers-page">
      <div className="page-header">
        <div>
          <h1>Servers</h1>
          <p>Monitor registered servers and their agent health.</p>
        </div>

        <div className="status-pill">
          <span className="status-dot status-dot-ok" />
          {servers.length} registered
        </div>
      </div>

      <div className="panel table-panel servers-table-panel">
        <div className="panel-title">Registered servers</div>

        {serversQuery.isLoading && <p className="muted-text">Loading servers...</p>}

        {serversQuery.isError && (
          <p className="muted-text">Failed to load servers from Manager API.</p>
        )}

        {serversQuery.isSuccess && servers.length === 0 && (
          <p className="muted-text">No servers registered yet.</p>
        )}

        {servers.length > 0 && (
          <div className="table servers-table">
            <div className="table-row table-head servers-table-row">
              <div>Name</div>
              <div>Provider</div>
              <div>Location</div>
              <div>Public IP</div>
              <div>Status</div>
              <div>Agent</div>
              <div>Version</div>
              <div>Last seen</div>
              <div>Created</div>
            </div>

            {servers.map((server) => (
              <ServerRow server={server} key={server.id} />
            ))}
          </div>
        )}
      </div>
    </section>
  );
}
