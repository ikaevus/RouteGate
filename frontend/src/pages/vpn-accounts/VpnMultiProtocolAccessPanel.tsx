import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  getVpnAccountClientConnection,
  type ClientProtocol,
  type VpnClientConnectionResponse,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';

type ProtocolConnection = {
  protocol: ClientProtocol;
  format: string;
  vlessLink?: string;
  wireGuardConfig?: string;
  hysteria2Uri?: string;
  shadowsocksUri?: string;
  mtprotoUri?: string;
  endpoint?: string;
  serverName?: string;
  network?: string;
  flow?: string;
};

type MultiProtocolConnection = VpnClientConnectionResponse & {
  connections?: ProtocolConnection[];
  profile: VpnClientConnectionResponse['profile'] & {
    enabledProtocols?: ClientProtocol[];
    activeProtocols?: ClientProtocol[];
  };
};

const protocolOrder: ClientProtocol[] = ['vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'];

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      title: 'Способы подключения',
      subtitle: 'Здесь показаны все настроенные для аккаунта протоколы. Рабочие подключения доступны сразу, а ожидающие применения становятся доступны только после успешной активации.',
      loading: 'Загрузка способов подключения...',
      error: 'Не удалось загрузить способы подключения.',
      empty: 'Протоколы подключения пока не настроены.',
      primary: 'Основной',
      active: 'Активен',
      pending: 'Ожидает применения',
      retiring: 'Активен до применения',
      pendingTitle: 'Подключение ещё не активно',
      pendingDescription: 'RouteGate подготовит данные подключения после успешного применения конфигурации.',
      retiringDescription: 'Этот способ подключения остаётся рабочим до успешного применения нового набора протоколов.',
      activeUnavailable: 'Протокол отмечен активным, но данные подключения временно недоступны. Обновите страницу или повторите применение конфигурации.',
      copy: 'Копировать',
      copied: 'Скопировано',
      endpoint: 'Endpoint',
      vless: 'VLESS / Reality',
      wireguard: 'WireGuard',
      hysteria2: 'Hysteria2',
      shadowsocks: 'Shadowsocks 2022',
      mtproto: 'MTProto / FakeTLS',
      vlessMaterial: 'VLESS-ссылка',
      wireguardMaterial: 'Конфигурация WireGuard',
      hysteria2Material: 'Hysteria2 URI',
      shadowsocksMaterial: 'Shadowsocks URI',
      mtprotoMaterial: 'Telegram proxy URI',
    } as const;
  }
  return {
    title: 'Connection methods',
    subtitle: 'All protocols configured for this account are shown here. Working connections are available immediately; pending ones become available only after successful activation.',
    loading: 'Loading connection methods...',
    error: 'Could not load connection methods.',
    empty: 'No connection protocols are configured yet.',
    primary: 'Primary',
    active: 'Active',
    pending: 'Pending apply',
    retiring: 'Active until apply',
    pendingTitle: 'Connection is not active yet',
    pendingDescription: 'RouteGate will provide connection data after the configuration is applied successfully.',
    retiringDescription: 'This connection method remains working until the new protocol set is applied successfully.',
    activeUnavailable: 'The protocol is marked active, but its connection data is temporarily unavailable. Refresh the page or retry the configuration apply.',
    copy: 'Copy',
    copied: 'Copied',
    endpoint: 'Endpoint',
    vless: 'VLESS / Reality', wireguard: 'WireGuard', hysteria2: 'Hysteria2',
    shadowsocks: 'Shadowsocks 2022', mtproto: 'MTProto / FakeTLS',
    vlessMaterial: 'VLESS link', wireguardMaterial: 'WireGuard configuration',
    hysteria2Material: 'Hysteria2 URI', shadowsocksMaterial: 'Shadowsocks URI',
    mtprotoMaterial: 'Telegram proxy URI',
  } as const;
}

function labelForProtocol(protocol: ClientProtocol, copy: ReturnType<typeof getCopy>): string {
  return copy[protocol];
}

function ordered(values: readonly ClientProtocol[]): ClientProtocol[] {
  const selected = new Set(values);
  return protocolOrder.filter((protocol) => selected.has(protocol));
}

function material(connection: ProtocolConnection, copy: ReturnType<typeof getCopy>): { label: string; value: string } {
  switch (connection.protocol) {
    case 'wireguard': return { label: copy.wireguardMaterial, value: connection.wireGuardConfig ?? '' };
    case 'hysteria2': return { label: copy.hysteria2Material, value: connection.hysteria2Uri ?? '' };
    case 'shadowsocks': return { label: copy.shadowsocksMaterial, value: connection.shadowsocksUri ?? '' };
    case 'mtproto': return { label: copy.mtprotoMaterial, value: connection.mtprotoUri ?? '' };
    case 'vless':
    default: return { label: copy.vlessMaterial, value: connection.vlessLink ?? '' };
  }
}

function legacyConnection(connection: MultiProtocolConnection): ProtocolConnection {
  return {
    protocol: connection.protocol,
    format: connection.format,
    vlessLink: connection.vlessLink,
    wireGuardConfig: connection.wireGuardConfig,
    hysteria2Uri: connection.hysteria2Uri,
    shadowsocksUri: connection.shadowsocksUri,
    mtprotoUri: connection.mtprotoUri,
    endpoint: connection.endpoint,
    serverName: connection.serverName,
    network: connection.network,
    flow: connection.flow,
  };
}

export function VpnMultiProtocolAccessPanel({ accountId }: { accountId: string }) {
  const copy = getCopy();
  const [copiedProtocol, setCopiedProtocol] = useState<ClientProtocol | null>(null);
  const query = useQuery({
    queryKey: ['vpn-account-client-connection', accountId],
    queryFn: () => getVpnAccountClientConnection(accountId) as Promise<MultiProtocolConnection>,
  });

  const connection = query.data;
  const connections = connection?.connections?.length
    ? connection.connections
    : connection ? [legacyConnection(connection)] : [];
  const activeProtocols = ordered(connection?.profile.activeProtocols?.length
    ? connection.profile.activeProtocols
    : connections.map((item) => item.protocol));
  const desiredProtocols = ordered(connection?.profile.enabledProtocols?.length
    ? connection.profile.enabledProtocols
    : activeProtocols);
  const visibleProtocols = ordered([...activeProtocols, ...desiredProtocols]);
  const activeSet = new Set(activeProtocols);
  const desiredSet = new Set(desiredProtocols);
  const connectionByProtocol = new Map(connections.map((item) => [item.protocol, item]));

  const copyMaterial = async (protocolConnection: ProtocolConnection) => {
    const value = material(protocolConnection, copy).value;
    if (!navigator.clipboard || !value.trim()) return;
    await navigator.clipboard.writeText(value);
    setCopiedProtocol(protocolConnection.protocol);
    window.setTimeout(() => setCopiedProtocol(null), 1800);
  };

  return (
    <div className="panel feature-detail-panel vpn-multi-protocol-access-panel">
      <div className="panel-header">
        <div>
          <div className="panel-title">{copy.title}</div>
          <p className="panel-subtitle">{copy.subtitle}</p>
        </div>
      </div>

      {query.isLoading && <p className="empty-state">{copy.loading}</p>}
      {query.isError && <div className="form-message form-message-error">{copy.error}</div>}
      {!query.isLoading && !query.isError && visibleProtocols.length === 0 && <p className="empty-state">{copy.empty}</p>}

      {visibleProtocols.length > 0 && (
        <div className="vpn-protocol-access-grid">
          {visibleProtocols.map((protocol) => {
            const protocolConnection = connectionByProtocol.get(protocol);
            const isActive = activeSet.has(protocol);
            const isDesired = desiredSet.has(protocol);
            const isPrimary = isActive && protocol === connection?.protocol;
            const isPending = isDesired && !isActive;
            const isRetiring = isActive && !isDesired;
            const access = protocolConnection ? material(protocolConnection, copy) : null;

            return (
              <section
                className={`vpn-protocol-access-card${isPending ? ' vpn-protocol-access-card-pending' : ''}${isRetiring ? ' vpn-protocol-access-card-retiring' : ''}`}
                key={protocol}
              >
                <div className="vpn-protocol-access-card-header">
                  <strong>{labelForProtocol(protocol, copy)}</strong>
                  <div className="vpn-protocol-access-card-statuses">
                    {isPrimary && <span className="status-pill">{copy.primary}</span>}
                    <span className={`status-pill vpn-protocol-access-state ${isPending ? 'vpn-protocol-access-state-pending' : isRetiring ? 'vpn-protocol-access-state-retiring' : 'vpn-protocol-access-state-active'}`}>
                      {isPending ? copy.pending : isRetiring ? copy.retiring : copy.active}
                    </span>
                  </div>
                </div>

                {isPending ? (
                  <div className="vpn-protocol-access-pending">
                    <strong>{copy.pendingTitle}</strong>
                    <span>{copy.pendingDescription}</span>
                  </div>
                ) : protocolConnection && access ? (
                  <>
                    {protocolConnection.endpoint && (
                      <div className="vpn-protocol-access-meta">
                        <span>{copy.endpoint}</span>
                        <code>{protocolConnection.endpoint}</code>
                      </div>
                    )}
                    <label className="field">
                      <span>{access.label}</span>
                      <textarea value={access.value} readOnly rows={protocol === 'wireguard' ? 10 : 3} />
                    </label>
                    <button
                      className="small-button"
                      type="button"
                      disabled={!access.value.trim()}
                      onClick={() => void copyMaterial(protocolConnection)}
                    >
                      {copiedProtocol === protocol ? copy.copied : copy.copy}
                    </button>
                    {isRetiring && <div className="vpn-protocol-access-retiring-note">{copy.retiringDescription}</div>}
                  </>
                ) : (
                  <div className="form-message form-message-warning">{copy.activeUnavailable}</div>
                )}
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}
