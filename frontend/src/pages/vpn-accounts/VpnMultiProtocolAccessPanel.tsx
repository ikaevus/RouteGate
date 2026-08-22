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
    activeProtocols?: ClientProtocol[];
  };
};

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      title: 'Активные способы подключения',
      subtitle: 'Каждый активный протокол — независимый рабочий способ подключения этого аккаунта. Можно настроить несколько клиентов и переключаться между ними без изменения аккаунта.',
      loading: 'Загрузка активных доступов...',
      error: 'Не удалось загрузить активные способы подключения.',
      empty: 'Активные протоколы пока не найдены.',
      primary: 'Основной',
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
      warning: 'Эти данные предоставляют доступ к VPN. Не публикуйте их.',
    } as const;
  }
  return {
    title: 'Active connection methods',
    subtitle: 'Each active protocol is an independent working connection method for this account. Configure several clients and switch between them without changing the account.',
    loading: 'Loading active access methods...',
    error: 'Could not load active connection methods.',
    empty: 'No active protocols were found yet.',
    primary: 'Primary',
    copy: 'Copy',
    copied: 'Copied',
    endpoint: 'Endpoint',
    vless: 'VLESS / Reality', wireguard: 'WireGuard', hysteria2: 'Hysteria2',
    shadowsocks: 'Shadowsocks 2022', mtproto: 'MTProto / FakeTLS',
    vlessMaterial: 'VLESS link', wireguardMaterial: 'WireGuard configuration',
    hysteria2Material: 'Hysteria2 URI', shadowsocksMaterial: 'Shadowsocks URI',
    mtprotoMaterial: 'Telegram proxy URI',
    warning: 'These credentials grant VPN access. Do not publish them.',
  } as const;
}

function labelForProtocol(protocol: ClientProtocol, copy: ReturnType<typeof getCopy>): string {
  return copy[protocol];
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
      {!query.isLoading && !query.isError && connections.length === 0 && <p className="empty-state">{copy.empty}</p>}

      {connections.length > 0 && (
        <>
          <div className="vpn-protocol-access-grid">
            {connections.map((protocolConnection) => {
              const access = material(protocolConnection, copy);
              const isPrimary = protocolConnection.protocol === connection?.protocol;
              return (
                <section className="vpn-protocol-access-card" key={protocolConnection.protocol}>
                  <div className="vpn-protocol-access-card-header">
                    <strong>{labelForProtocol(protocolConnection.protocol, copy)}</strong>
                    {isPrimary && <span className="status-pill">{copy.primary}</span>}
                  </div>
                  {protocolConnection.endpoint && (
                    <div className="vpn-protocol-access-meta">
                      <span>{copy.endpoint}</span>
                      <code>{protocolConnection.endpoint}</code>
                    </div>
                  )}
                  <label className="field">
                    <span>{access.label}</span>
                    <textarea value={access.value} readOnly rows={protocolConnection.protocol === 'wireguard' ? 10 : 3} />
                  </label>
                  <button
                    className="small-button"
                    type="button"
                    disabled={!access.value.trim()}
                    onClick={() => void copyMaterial(protocolConnection)}
                  >
                    {copiedProtocol === protocolConnection.protocol ? copy.copied : copy.copy}
                  </button>
                </section>
              );
            })}
          </div>
          <div className="form-message form-message-warning">{copy.warning}</div>
        </>
      )}
    </div>
  );
}
