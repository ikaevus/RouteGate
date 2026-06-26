import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getServers, type Server } from '../../entities/server/api/serverApi';
import { t } from '../../shared/i18n/i18n';
import { EmptyState } from '../../shared/ui/EmptyState';
import { StatusBadge } from '../../shared/ui/StatusBadge';

function formatDate(value?: string | null): string {
  if (!value) {
    return t('common.notAvailable');
  }

  return new Date(value).toLocaleString();
}

function formatValue(value?: string | null): string {
  return value && value.trim() !== '' ? value : t('common.notAvailable');
}

function ServerRow({ server }: { server: Server }) {
  return (
    <Link className="table-row servers-table-row table-row-link" to={`/servers/${server.id}`}>
      <div>
        <strong className="text-link">{formatValue(server.name)}</strong>
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
          <span className="muted-text">{t('common.notAvailable')}</span>
        )}
      </div>
      <div>{formatValue(server.agent?.agentVersion)}</div>
      <div>{formatDate(server.agent?.lastSeenAt)}</div>
      <div>{formatDate(server.createdAt)}</div>
    </Link>
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
          <h1>{t('servers.title')}</h1>
          <p>{t('servers.subtitle')}</p>
        </div>

        <div className="status-pill">
          <span className="status-dot status-dot-ok" />
          {servers.length} {t('servers.registered')}
        </div>
      </div>

      <div className="panel table-panel servers-table-panel">
        <div className="panel-title">{t('servers.panelTitle')}</div>

        {serversQuery.isLoading && <p className="empty-state">{t('servers.loading')}</p>}

        {serversQuery.isError && (
          <div className="form-message form-message-error">{t('servers.loadError')}</div>
        )}

        {serversQuery.isSuccess && servers.length === 0 && (
          <EmptyState title={t('servers.emptyTitle')} description={t('servers.emptyDescription')} />
        )}

        {servers.length > 0 && (
          <div className="table servers-table">
            <div className="table-row table-head servers-table-row">
              <div>{t('servers.name')}</div>
              <div>{t('servers.provider')}</div>
              <div>{t('servers.location')}</div>
              <div>{t('servers.publicIp')}</div>
              <div>{t('servers.status')}</div>
              <div>{t('servers.agent')}</div>
              <div>{t('servers.version')}</div>
              <div>{t('servers.lastSeen')}</div>
              <div>{t('servers.created')}</div>
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
