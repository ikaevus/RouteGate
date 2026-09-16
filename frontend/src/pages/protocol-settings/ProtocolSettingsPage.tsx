import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { getServers, type Server } from '../../entities/server/api/serverApi';
import { ServerProtocolSettingsPanel } from '../servers/ServerProtocolSettingsPanel';
import { getCurrentLocale, t } from '../../shared/i18n/i18n';
import { EmptyState } from '../../shared/ui/EmptyState';
import { StatusBadge } from '../../shared/ui/StatusBadge';

const copy = {
  en: {
    subtitle: 'Configure VLESS / Reality, WireGuard, Hysteria2, Shadowsocks 2022, and MTProto on VPN-capable nodes.',
    emptyDescription: 'Add a VPN or Hybrid node before configuring managed VPN protocols.',
    selectDescription: 'Choose a VPN or Hybrid node to view and edit its managed protocol settings.',
    unavailable: 'This node cannot host VPN protocols. Management Nodes are control-plane only.',
    hysteriaHybridNotice: 'On this Hybrid Node, Hysteria2 requires a dedicated hostname different from the Manager hostname and a direct DNS record to the node (no CDN/proxy). nginx forwards only its ACME HTTP-01 challenge to the loopback Hysteria service; TCP 443 remains owned by RouteGate and UDP 443 by Hysteria2.',
  },
  ru: {
    subtitle: 'Настраивайте VLESS / Reality, WireGuard, Hysteria2, Shadowsocks 2022 и MTProto на VPN-узлах.',
    emptyDescription: 'Добавьте VPN-узел или гибридный узел, прежде чем настраивать управляемые VPN-протоколы.',
    selectDescription: 'Выберите VPN-узел или гибридный узел, чтобы просмотреть и изменить настройки протоколов.',
    unavailable: 'Этот узел не может размещать VPN-протоколы. Management Node относится только к плоскости управления.',
    hysteriaHybridNotice: 'На этом Hybrid Node для Hysteria2 нужен отдельный домен, отличный от домена Manager, и прямая DNS-запись на узел без CDN/proxy. nginx передаёт Hysteria только ACME HTTP-01 challenge через loopback: TCP 443 остаётся у RouteGate, а UDP 443 используется Hysteria2.',
  },
} as const;

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
  const text = copy[getCurrentLocale()];

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
  });

  const servers = (serversQuery.data?.items ?? []).filter(
    (server) => server.deploymentRole !== 'management',
  );
  const selectedServer = servers.find((server) => server.id === serverId);

  return (
    <section className="page protocol-settings-page">
      <div className="page-header">
        <div>
          <h1>{t('protocolSettings.title')}</h1>
          <p>{text.subtitle}</p>
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
              description={text.emptyDescription}
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
              <div className="form-message form-message-error">{text.unavailable}</div>
            )}
            {selectedServer?.deploymentRole === 'hybrid' && (
              <div className="form-message form-message-warning">{text.hysteriaHybridNotice}</div>
            )}
            {selectedServer && <ServerProtocolSettingsPanel serverId={selectedServer.id} />}
          </>
        ) : (
          <div className="panel">
            <EmptyState
              title={t('protocolSettings.selectTitle')}
              description={text.selectDescription}
            />
          </div>
        )}
      </div>
    </section>
  );
}
