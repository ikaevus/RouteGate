import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { getProtocolSettings, getServers } from '../../entities/server/api/serverApi';
import {
  getVpnAccountClientConnection,
  updateVpnAccountClientProfile,
  type ClientProtocolPreference,
  type UpdateVpnClientProfileRequest,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { getVpnAccount } from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';
import {
  deployPendingProtocol,
  ensureProtocolRuntime,
  type ProtocolDeploymentStage,
} from './protocolDeploymentWorkflow';

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
      save: 'Переключить протокол',
      retry: 'Повторить активацию',
      saving: 'Подготовка...',
      saved: 'Протокол успешно развернут и активирован.',
      pending: 'Предпочтение сохранено, но новый протокол ещё не активирован. Текущее рабочее подключение сохранено.',
      deploy: 'RouteGate проверит нужный runtime, при необходимости установит его, сформирует конфигурацию и активирует новый протокол только после успешного применения.',
      loading: 'Загрузка протокола...',
      loadError: 'Не удалось определить клиентский профиль. Если аккаунту ещё не назначен сервер, сначала назначьте VPN-узел.',
      saveError: 'Не удалось завершить переключение. Рабочий протокол остаётся активным; выбранное предпочтение можно повторно применить после устранения причины.',
      errorDetail: 'Причина',
      noServer: 'Сначала назначьте этому аккаунту VPN-узел.',
      stages: {
        saving_preference: 'Сохраняю предпочтение…',
        checking_runtime: 'Проверяю VPN runtime на узле…',
        installing_runtime: 'Устанавливаю необходимый VPN runtime…',
        rendering_config: 'Формирую конфигурацию узла…',
        validating_config: 'Проверяю конфигурацию…',
        applying_config: 'Передаю конфигурацию Agent…',
        waiting_for_apply: 'Жду применения и healthcheck…',
        completed: 'Новый протокол активирован.',
      },
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
    save: 'Switch protocol',
    retry: 'Retry activation',
    saving: 'Preparing...',
    saved: 'Protocol deployed and activated successfully.',
    pending: 'The preference is saved, but the new protocol is not active yet. The current working connection has been preserved.',
    deploy: 'RouteGate will verify the required runtime, install it when needed, render the node configuration, and activate the new protocol only after a successful apply.',
    loading: 'Loading protocol...',
    loadError: 'Could not resolve the client profile. If the account has no server yet, assign a VPN node first.',
    saveError: 'The switch could not be completed. The working protocol remains active; retry the selected preference after resolving the reported condition.',
    errorDetail: 'Reason',
    noServer: 'Assign a VPN node to this account first.',
    stages: {
      saving_preference: 'Saving protocol preference…',
      checking_runtime: 'Checking VPN runtime on the node…',
      installing_runtime: 'Installing the required VPN runtime…',
      rendering_config: 'Rendering node configuration…',
      validating_config: 'Validating configuration…',
      applying_config: 'Sending configuration to Agent…',
      waiting_for_apply: 'Waiting for apply and healthcheck…',
      completed: 'New protocol activated.',
    },
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

function mutationErrorDetail(error: unknown): string {
  if (error instanceof Error) {
    return error.message.trim();
  }
  if (typeof error === 'string') {
    return error.trim();
  }
  return '';
}

export function VpnAccountProtocolPreferencePanel({ accountId }: Props) {
  const copy = getCopy();
  const queryClient = useQueryClient();
  const queryKey = ['vpn-account-client-connection', accountId] as const;
  const [protocol, setProtocol] = useState<ClientProtocolPreference>('auto');
  const [saved, setSaved] = useState(false);
  const [deploymentStage, setDeploymentStage] = useState<ProtocolDeploymentStage | null>(null);

  const connectionQuery = useQuery({
    queryKey,
    queryFn: () => getVpnAccountClientConnection(accountId),
  });
  const accountQuery = useQuery({
    queryKey: ['vpn-account', accountId],
    queryFn: () => getVpnAccount(accountId),
  });
  const serversQuery = useQuery({ queryKey: ['servers'], queryFn: getServers });
  const assignedServerId = accountQuery.data?.serverId ?? '';
  const protocolSettingsQuery = useQuery({
    queryKey: ['server-protocol-settings', assignedServerId],
    queryFn: () => getProtocolSettings(assignedServerId),
    enabled: Boolean(assignedServerId),
  });

  useEffect(() => {
    const preference = connectionQuery.data?.profile.protocol;
    if (preference) {
      setProtocol(preference);
    }
  }, [connectionQuery.data?.profile.protocol]);

  useEffect(() => {
    setSaved(false);
    setDeploymentStage(null);
  }, [accountId]);

  const assignedServer = (serversQuery.data?.items ?? []).find(
    (server) => server.id === assignedServerId,
  );
  const hysteria2TopologyBlocked = assignedServer?.deploymentRole === 'hybrid';
  const selectedTopologyBlocked = hysteria2TopologyBlocked && protocol === 'hysteria2';

  const saveMutation = useMutation({
    mutationFn: async () => {
      const profile = connectionQuery.data?.profile;
      if (!profile) {
        throw new Error(copy.loadError);
      }
      if (!assignedServer) {
        throw new Error(copy.noServer);
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

      setDeploymentStage('saving_preference');
      await updateVpnAccountClientProfile(accountId, request);

      let runtimeProtocol = protocol;
      if (runtimeProtocol === 'auto') {
        const settings = protocolSettingsQuery.data ?? await getProtocolSettings(assignedServer.id);
        runtimeProtocol = settings.protocol as ClientProtocolPreference;
      }
      await ensureProtocolRuntime(assignedServer, runtimeProtocol, setDeploymentStage);
      await deployPendingProtocol(assignedServer.id, setDeploymentStage);

      const connection = await getVpnAccountClientConnection(accountId);
      if (connection.protocol !== runtimeProtocol) {
        throw new Error('protocol_activation_not_confirmed');
      }
      return connection;
    },
    onMutate: () => {
      setSaved(false);
      setDeploymentStage('saving_preference');
    },
    onSuccess: async (connection) => {
      queryClient.setQueryData(queryKey, connection);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['vpn-account-credentials', accountId] }),
        queryClient.invalidateQueries({ queryKey: ['vpn-account-routing-policy', accountId] }),
        queryClient.invalidateQueries({ queryKey: ['vpn-account', accountId] }),
        queryClient.invalidateQueries({ queryKey: ['servers'] }),
        queryClient.invalidateQueries({ queryKey: ['server-protocol-settings', assignedServerId] }),
        queryClient.invalidateQueries({ queryKey: ['server-config-versions', assignedServerId] }),
        queryClient.invalidateQueries({ queryKey: ['server-config-apply-jobs', assignedServerId] }),
      ]);
      setProtocol(connection.profile.protocol ?? 'auto');
      setDeploymentStage('completed');
      setSaved(true);
      window.setTimeout(() => setSaved(false), 2600);
    },
    onError: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey }),
        queryClient.invalidateQueries({ queryKey: ['servers'] }),
      ]);
    },
  });

  const currentPreference = connectionQuery.data?.profile.protocol ?? 'auto';
  const desiredEffectiveProtocol = protocol === 'auto'
    ? protocolSettingsQuery.data?.protocol
    : protocol;
  const activationPending = Boolean(
    desiredEffectiveProtocol
    && connectionQuery.data?.protocol
    && connectionQuery.data.protocol !== desiredEffectiveProtocol,
  );
  const changed = protocol !== currentPreference;
  const stageText = deploymentStage ? copy.stages[deploymentStage] : null;
  const canRetryActivation = !changed && activationPending;
  const errorDetail = mutationErrorDetail(saveMutation.error);

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
                disabled={saveMutation.isPending}
                onChange={(event) => {
                  saveMutation.reset();
                  setSaved(false);
                  setDeploymentStage(null);
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

          {selectedTopologyBlocked && (
            <div className="form-message form-message-warning">{copy.hysteria2Topology}</div>
          )}
          <div className="form-message form-message-warning">{copy.deploy}</div>
          {canRetryActivation && !saveMutation.isPending && (
            <div className="form-message form-message-warning">{copy.pending}</div>
          )}

          {saveMutation.isPending && stageText && (
            <div className="form-message" role="status">{stageText}</div>
          )}
          {saveMutation.isError && (
            <div className="form-message form-message-error">
              {copy.saveError}
              {errorDetail && <div>{copy.errorDetail}: {errorDetail}</div>}
            </div>
          )}
          {saved && <div className="form-message" role="status">{copy.saved}</div>}

          <div className="form-actions">
            <button
              className="primary-button"
              type="button"
              disabled={(!changed && !activationPending) || saveMutation.isPending || selectedTopologyBlocked || !assignedServer}
              onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending ? copy.saving : canRetryActivation ? copy.retry : copy.save}
            </button>
          </div>
        </>
      )}
    </div>
  );
}
