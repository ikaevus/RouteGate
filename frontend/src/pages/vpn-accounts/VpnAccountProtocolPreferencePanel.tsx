import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { getServers } from '../../entities/server/api/serverApi';
import {
  getVpnAccountClientConnection,
  updateVpnAccountClientProfile,
  type ClientProtocolPreference,
  type UpdateVpnClientProfileRequest,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { getVpnAccount } from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';

type Props = {
  accountId: string;
};

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      title: 'Протокол подключения',
      subtitle: 'Выберите протокол для этого VPN-аккаунта. Auto использует протокол узла по умолчанию.',
      preference: 'Предпочтение',
      effective: 'Сейчас используется',
      auto: 'Auto — протокол узла по умолчанию',
      vless: 'VLESS / Reality',
      wireguard: 'WireGuard',
      hysteria2: 'Hysteria2',
      hysteria2Dedicated: 'Hysteria2 — только отдельный VPN Node',
      hysteria2Topology: 'Hysteria2 использует отдельный ACME HTTP-01 lifecycle сертификата и сейчас поддерживается только на dedicated VPN Node. Назначьте аккаунт отдельному VPN Node или выберите другой протокол.',
      shadowsocks: 'Shadowsocks 2022',
      mtproto: 'MTProto / FakeTLS',
      save: 'Применить протокол',
      saving: 'Применение...',
      saved: 'Протокол сохранён.',
      deploy: 'После смены протокола сформируйте и примените конфигурацию назначенного VPN-узла.',
      loading: 'Загрузка протокола...',
      loadError: 'Не удалось определить клиентский профиль. Если аккаунту ещё не назначен сервер, сначала назначьте VPN-узел.',
      saveError: 'Не удалось сохранить протокол подключения.',
    } as const;
  }

  return {
    title: 'Connection protocol',
    subtitle: 'Choose the protocol for this VPN account. Auto follows the node default.',
    preference: 'Preference',
    effective: 'Effective now',
    auto: 'Auto — inherit node default',
    vless: 'VLESS / Reality',
    wireguard: 'WireGuard',
    hysteria2: 'Hysteria2',
    hysteria2Dedicated: 'Hysteria2 — dedicated VPN Node only',
    hysteria2Topology: 'Hysteria2 uses its own ACME HTTP-01 certificate lifecycle and is currently supported only on a dedicated VPN Node. Assign the account to a dedicated VPN Node or choose another protocol.',
    shadowsocks: 'Shadowsocks 2022',
    mtproto: 'MTProto / FakeTLS',
    save: 'Apply protocol',
    saving: 'Applying...',
    saved: 'Protocol preference saved.',
    deploy: 'After changing the protocol, render and deploy the assigned VPN node configuration.',
    loading: 'Loading protocol...',
    loadError: 'Could not resolve the client profile. If the account has no server yet, assign a VPN node first.',
    saveError: 'Could not save the connection protocol.',
  } as const;
}

function protocolLabel(protocol: string, copy: ReturnType<typeof getCopy>): string {
  switch (protocol) {
    case 'wireguard':
      return copy.wireguard;
    case 'hysteria2':
      return copy.hysteria2;
    case 'shadowsocks':
      return copy.shadowsocks;
    case 'mtproto':
      return copy.mtproto;
    case 'vless':
      return copy.vless;
    default:
      return protocol || '—';
  }
}

export function VpnAccountProtocolPreferencePanel({ accountId }: Props) {
  const copy = getCopy();
  const queryClient = useQueryClient();
  const queryKey = ['vpn-account-client-connection', accountId] as const;
  const [protocol, setProtocol] = useState<ClientProtocolPreference>('auto');
  const [saved, setSaved] = useState(false);

  const connectionQuery = useQuery({
    queryKey,
    queryFn: () => getVpnAccountClientConnection(accountId),
  });
  const accountQuery = useQuery({
    queryKey: ['vpn-account', accountId],
    queryFn: () => getVpnAccount(accountId),
  });
  const serversQuery = useQuery({ queryKey: ['servers'], queryFn: getServers });

  useEffect(() => {
    const preference = connectionQuery.data?.profile.protocol;
    if (preference) {
      setProtocol(preference);
    }
  }, [connectionQuery.data?.profile.protocol]);

  useEffect(() => {
    setSaved(false);
  }, [accountId]);

  const assignedServer = (serversQuery.data?.items ?? []).find(
    (server) => server.id === accountQuery.data?.serverId,
  );
  const hysteria2TopologyBlocked = assignedServer?.deploymentRole === 'hybrid';
  const selectedTopologyBlocked = hysteria2TopologyBlocked && protocol === 'hysteria2';

  const saveMutation = useMutation({
    mutationFn: () => {
      const profile = connectionQuery.data?.profile;
      if (!profile) {
        throw new Error(copy.loadError);
      }
      if (selectedTopologyBlocked) {
        throw new Error(copy.hysteria2Topology);
      }
      const request: UpdateVpnClientProfileRequest = {
        name: profile.name,
        clientType: profile.clientType,
        deviceType: profile.deviceType,
        fingerprintMode: profile.fingerprintMode,
        fingerprint: profile.fingerprint,
        serverNameOverride: profile.serverNameOverride ?? '',
        spiderX: profile.spiderX || '/',
        mtu: profile.mtu ?? null,
        protocol,
      };
      return updateVpnAccountClientProfile(accountId, request);
    },
    onMutate: () => setSaved(false),
    onSuccess: async (connection) => {
      queryClient.setQueryData(queryKey, connection);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['vpn-account-credentials', accountId] }),
        queryClient.invalidateQueries({ queryKey: ['vpn-account-routing-policy', accountId] }),
        queryClient.invalidateQueries({ queryKey: ['servers'] }),
      ]);
      setProtocol(connection.profile.protocol ?? 'auto');
      setSaved(true);
      window.setTimeout(() => setSaved(false), 2200);
    },
  });

  const currentPreference = connectionQuery.data?.profile.protocol ?? 'auto';
  const changed = protocol !== currentPreference;

  return (
    <div className="panel feature-detail-panel vpn-account-protocol-preference-panel">
      <div className="panel-header">
        <div>
          <div className="panel-title">{copy.title}</div>
          <p className="panel-subtitle">{copy.subtitle}</p>
        </div>
        {connectionQuery.data && (
          <span className="status-pill">
            {copy.effective}: {protocolLabel(connectionQuery.data.protocol, copy)}
          </span>
        )}
      </div>

      {connectionQuery.isLoading && <p className="empty-state">{copy.loading}</p>}
      {connectionQuery.isError && <div className="form-message form-message-error">{copy.loadError}</div>}

      {connectionQuery.data && (
        <>
          <div className="vpn-account-create-grid">
            <label className="field">
              <span>{copy.preference}</span>
              <select
                value={protocol}
                onChange={(event) => {
                  setSaved(false);
                  setProtocol(event.target.value as ClientProtocolPreference);
                }}
              >
                <option value="auto">{copy.auto}</option>
                <option value="vless">{copy.vless}</option>
                <option value="wireguard">{copy.wireguard}</option>
                <option value="hysteria2" disabled={hysteria2TopologyBlocked}>
                  {hysteria2TopologyBlocked ? copy.hysteria2Dedicated : copy.hysteria2}
                </option>
                <option value="shadowsocks">{copy.shadowsocks}</option>
                <option value="mtproto">{copy.mtproto}</option>
              </select>
            </label>
          </div>

          {hysteria2TopologyBlocked && (
            <div className="form-message form-message-warning">{copy.hysteria2Topology}</div>
          )}
          <div className="form-message form-message-warning">{copy.deploy}</div>

          {saveMutation.isError && (
            <div className="form-message form-message-error">{copy.saveError}</div>
          )}
          {saved && <div className="form-message" role="status">{copy.saved}</div>}

          <div className="form-actions">
            <button
              className="primary-button"
              type="button"
              disabled={!changed || saveMutation.isPending || selectedTopologyBlocked}
              onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending ? copy.saving : copy.save}
            </button>
          </div>
        </>
      )}
    </div>
  );
}
