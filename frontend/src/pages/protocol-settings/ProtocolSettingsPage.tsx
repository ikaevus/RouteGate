import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { getServers, type Server } from '../../entities/server/api/serverApi';
import { ServerProtocolSettingsPanel } from '../servers/ServerProtocolSettingsPanel';
import { t } from '../../shared/i18n/i18n';
import { EmptyState } from '../../shared/ui/EmptyState';
import { StatusBadge } from '../../shared/ui/StatusBadge';

function formatValue(value?: string | null): string {
  return value && value.trim() !== '' ? value : t('common.notAvailable');
}

function hasValue(value?: string | null): value is string {
  return Boolean(value?.trim());
}

function ServerSettingsRow({ server, selected }: { server: Server; selected: boolean }) {
  return (
    <Link
      className={`admin-table-row protocol-server-table-row vpn-account-row-link${selected ? ' vpn-account-row-selected' : ''}`}
      to={`/protocol-settings/${server.id}`}
    >
      <div className="protocol-server-identity">
        <strong>{formatValue(server.name)}</strong>
        {hasValue(server.description) && <span>{server.description}</span>}
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
          <h1>{t('protocolSettings.title')}</h1>
          <p>{t('protocolSettings.subtitle')}</p>
        </div>

        <div className="status-pill">
          <span className="status-dot status-dot-ok" />
          {servers.length} {t('protocolSettings.servers')}
        </div>
      </div>

      <div className="protocol-settings-layout">
        <div className="panel admin-table-panel">
          <div className="panel-title">{t('protocolSettings.panelTitle')}</div>

          {serversQuery.isLoading && <p className="empty-state">{t('protocolSettings.loading')}</p>}

          {serversQuery.isError && (
            <div className="form-message form-message-error">{t('protocolSettings.loadError')}</div>
          )}

          {serversQuery.isSuccess && servers.length === 0 && (
            <EmptyState
              title={t('protocolSettings.emptyTitle')}
              description={t('protocolSettings.emptyDescription')}
            />
          )}

          {servers.length > 0 && (
            <div className="admin-table protocol-server-table">
              <div className="admin-table-row admin-table-head protocol-server-table-row">
                <span>{t('servers.name')}</span>
                <span>{t('servers.provider')}</span>
                <span>{t('servers.location')}</span>
                <span>{t('servers.publicIp')}</span>
                <span>{t('servers.status')}</span>
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
              <div className="form-message form-message-error">{t('protocolSettings.serverNotFound')}</div>
            )}
            <ServerProtocolSettingsPanel serverId={serverId} />
          </>
        ) : (
          <div className="panel">
            <EmptyState
              title={t('protocolSettings.selectTitle')}
              description={t('protocolSettings.selectDescription')}
            />
          </div>
        )}
      </div>
    </section>
  );
}
