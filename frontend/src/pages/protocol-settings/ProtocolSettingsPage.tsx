import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { getServers, type Server } from '../../entities/server/api/serverApi';
import { ServerProtocolSettingsPanel } from '../servers/ServerProtocolSettingsPanel';

function formatValue(value?: string | null): string {
  return value && value.trim() !== '' ? value : '-';
}

function StatusBadge({ status }: { status?: string | null }) {
  const normalizedStatus = status && status.trim() !== '' ? status : 'unknown';
  const statusClassName = normalizedStatus.toLowerCase().replace(/[^a-z0-9-]/g, '-');

  return <span className={`badge badge-${statusClassName}`}>{normalizedStatus}</span>;
}

function ServerSettingsRow({ server, selected }: { server: Server; selected: boolean }) {
  return (
    <Link
      className={`admin-table-row protocol-server-table-row vpn-account-row-link${selected ? ' vpn-account-row-selected' : ''}`}
      to={`/protocol-settings/${server.id}`}
    >
      <div>
        <strong>{formatValue(server.name)}</strong>
        <span>{formatValue(server.description)}</span>
      </div>
      <span>{formatValue(server.provider)}</span>
      <span>{formatValue(server.location)}</span>
      <span>{formatValue(server.publicIp)}</span>
      <StatusBadge status={server.status} />
    </Link>
  );
}

export function ProtocolSettingsPage() {
  const { serverId } = useParams<{ serverId: string }>();

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
  });

  const servers = serversQuery.data?.items ?? [];
  const selectedServer = servers.find((server) => server.id === serverId);

  return (
    <section className="page protocol-settings-page">
      <div className="page-header">
        <div>
          <h1>Protocol Settings</h1>
          <p>Manage server VLESS / Reality settings used by account credentials.</p>
        </div>

        <div className="status-pill">
          <span className="status-dot status-dot-ok" />
          {servers.length} servers
        </div>
      </div>

      <div className="protocol-settings-layout">
        <div className="panel admin-table-panel">
          <div className="panel-title">Servers</div>

          {serversQuery.isLoading && <p className="empty-state">Loading servers...</p>}

          {serversQuery.isError && (
            <div className="form-message form-message-error">Failed to load servers.</div>
          )}

          {serversQuery.isSuccess && servers.length === 0 && (
            <p className="empty-state">No servers registered yet.</p>
          )}

          {servers.length > 0 && (
            <div className="admin-table protocol-server-table">
              <div className="admin-table-row admin-table-head protocol-server-table-row">
                <span>Server</span>
                <span>Provider</span>
                <span>Location</span>
                <span>Public IP</span>
                <span>Status</span>
              </div>
              {servers.map((server) => (
                <ServerSettingsRow
                  key={server.id}
                  selected={server.id === serverId}
                  server={server}
                />
              ))}
            </div>
          )}
        </div>

        {serverId ? (
          <>
            {serversQuery.isSuccess && !selectedServer && (
              <div className="form-message form-message-error">Selected server is not in the current list.</div>
            )}
            <ServerProtocolSettingsPanel serverId={serverId} />
          </>
        ) : (
          <div className="panel">
            <p className="empty-state">Select a server to view and edit protocol settings.</p>
          </div>
        )}
      </div>
    </section>
  );
}
