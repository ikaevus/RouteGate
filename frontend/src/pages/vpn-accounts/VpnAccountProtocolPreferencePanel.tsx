import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { getProtocolSettings, getServers } from '../../entities/server/api/serverApi';
import {
  getVpnAccountClientConnection,
  updateVpnAccountClientProfile,
  type ClientProtocol,
  type ClientProtocolPreference,
  type UpdateVpnClientProfileRequest,
  type VpnClientConnectionResponse,
  type VpnClientProfile,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { getVpnAccount } from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';
import {
  deployPendingProtocol,
  ensureProtocolRuntime,
  type ProtocolDeploymentStage,
} from './protocolDeploymentWorkflow';

type Props = { accountId: string };

type MultiProtocolProfile = VpnClientProfile & {
  enabledProtocols?: ClientProtocol[];
  activeProtocols?: ClientProtocol[];
};

type MultiProtocolConnection = VpnClientConnectionResponse & {
  profile: MultiProtocolProfile;
};

type MultiProtocolUpdate = UpdateVpnClientProfileRequest & {
  enabledProtocols: ClientProtocol[];
};

const protocolOrder: ClientProtocol[] = ['vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'];

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      title: 'Протоколы подключения',
      subtitle: 'Один VPN-аккаунт может использовать несколько протоколов одновременно. Добавление нового протокола не отключает уже работающие подключения.',
      enabled: 'Разрешённые протоколы',
      primary: 'Основной протокол',
      primaryHint: 'Основной протокол используется как вариант по умолчанию в местах, где нужен один способ подключения. Остальные разрешённые протоколы продолжают работать параллельно.',
      active: 'Активны сейчас',
      desired: 'Будут активны после применения',
      auto: 'Auto — протокол узла по умолчанию',
      vless: 'VLESS / Reality', wireguard: 'WireGuard', hysteria2: 'Hysteria2',
      shadowsocks: 'Shadowsocks 2022', mtproto: 'MTProto / FakeTLS',
      hysteria2Dedicated: 'Hysteria2 доступен только на отдельном VPN Node.',
      safety: 'Изменения применяются транзакционно: текущий рабочий набор остаётся активным до успешного render → validate → apply → healthcheck.',
      save: 'Применить набор протоколов', retry: 'Повторить применение', saving: 'Подготовка...',
      saved: 'Набор протоколов успешно применён.',
      pending: 'Новый набор сохранён как желаемый, но ещё не активирован. Предыдущие рабочие подключения сохранены.',
      loading: 'Загрузка протоколов...', loadError: 'Не удалось загрузить настройки протоколов.',
      saveError: 'Не удалось применить набор. Предыдущие активные протоколы должны остаться рабочими.',
      noServer: 'Сначала назначьте аккаунту VPN-узел.',
      selectOne: 'Должен быть выбран хотя бы один протокол.',
      autoNeedsDefault: 'Для Auto протокол узла по умолчанию должен входить в разрешённый набор.',
      errorDetail: 'Причина',
      stages: {
        saving_preference: 'Сохраняю желаемый набор…', checking_runtime: 'Проверяю VPN runtimes…',
        installing_runtime: 'Устанавливаю необходимый runtime…', rendering_config: 'Формирую общую конфигурацию узла…',
        validating_config: 'Проверяю конфигурацию…', applying_config: 'Передаю конфигурацию Agent…',
        waiting_for_apply: 'Жду применения и healthcheck…', completed: 'Набор протоколов активирован.',
      },
    } as const;
  }
  return {
    title: 'Connection protocols',
    subtitle: 'One VPN account can use several protocols at the same time. Enabling another protocol does not disable existing working connections.',
    enabled: 'Enabled protocols', primary: 'Primary protocol',
    primaryHint: 'The primary protocol is the default where one connection method is required. Other enabled protocols remain available in parallel.',
    active: 'Active now', desired: 'Active after apply', auto: 'Auto — inherit node default',
    vless: 'VLESS / Reality', wireguard: 'WireGuard', hysteria2: 'Hysteria2',
    shadowsocks: 'Shadowsocks 2022', mtproto: 'MTProto / FakeTLS',
    hysteria2Dedicated: 'Hysteria2 is available only on a dedicated VPN Node.',
    safety: 'Changes are transactional: the current working set stays active until render → validate → apply → healthcheck succeeds.',
    save: 'Apply protocol set', retry: 'Retry apply', saving: 'Preparing...', saved: 'Protocol set applied successfully.',
    pending: 'The desired set is saved but not active yet. Previous working connections are preserved.',
    loading: 'Loading protocols...', loadError: 'Could not load protocol settings.', noServer: 'Assign a VPN node first.',
    saveError: 'The protocol set could not be applied. Previous active protocols should remain working.',
    selectOne: 'Select at least one protocol.', autoNeedsDefault: 'Auto requires the node-default protocol to be included in the enabled set.',
    errorDetail: 'Reason',
    stages: {
      saving_preference: 'Saving desired protocol set…', checking_runtime: 'Checking VPN runtimes…',
      installing_runtime: 'Installing required runtime…', rendering_config: 'Rendering combined node configuration…',
      validating_config: 'Validating configuration…', applying_config: 'Sending configuration to Agent…',
      waiting_for_apply: 'Waiting for apply and healthcheck…', completed: 'Protocol set activated.',
    },
  } as const;
}

function protocolLabel(protocol: string, copy: ReturnType<typeof getCopy>): string {
  switch (protocol) {
    case 'vless': return copy.vless;
    case 'wireguard': return copy.wireguard;
    case 'hysteria2': return copy.hysteria2;
    case 'shadowsocks': return copy.shadowsocks;
    case 'mtproto': return copy.mtproto;
    default: return protocol || '—';
  }
}

function ordered(values: readonly ClientProtocol[]): ClientProtocol[] {
  const selected = new Set(values);
  return protocolOrder.filter((protocol) => selected.has(protocol));
}

function sameProtocols(left: readonly ClientProtocol[], right: readonly ClientProtocol[]): boolean {
  const a = ordered(left);
  const b = ordered(right);
  return a.length === b.length && a.every((value, index) => value === b[index]);
}

function mutationErrorDetail(error: unknown): string {
  return error instanceof Error ? error.message.trim() : typeof error === 'string' ? error.trim() : '';
}

export function VpnAccountProtocolPreferencePanel({ accountId }: Props) {
  const copy = getCopy();
  const queryClient = useQueryClient();
  const queryKey = ['vpn-account-client-connection', accountId] as const;
  const [primary, setPrimary] = useState<ClientProtocolPreference>('auto');
  const [enabledProtocols, setEnabledProtocols] = useState<ClientProtocol[]>(['vless']);
  const [saved, setSaved] = useState(false);
  const [deploymentStage, setDeploymentStage] = useState<ProtocolDeploymentStage | null>(null);

  const connectionQuery = useQuery({
    queryKey,
    queryFn: () => getVpnAccountClientConnection(accountId) as Promise<MultiProtocolConnection>,
  });
  const accountQuery = useQuery({ queryKey: ['vpn-account', accountId], queryFn: () => getVpnAccount(accountId) });
  const serversQuery = useQuery({ queryKey: ['servers'], queryFn: getServers });
  const assignedServerId = accountQuery.data?.serverId ?? '';
  const protocolSettingsQuery = useQuery({
    queryKey: ['server-protocol-settings', assignedServerId],
    queryFn: () => getProtocolSettings(assignedServerId),
    enabled: Boolean(assignedServerId),
  });

  const assignedServer = (serversQuery.data?.items ?? []).find((server) => server.id === assignedServerId);
  const hysteria2Blocked = assignedServer?.deploymentRole === 'hybrid';
  const nodeDefault = (protocolSettingsQuery.data?.protocol ?? 'vless') as ClientProtocol;

  useEffect(() => {
    const profile = connectionQuery.data?.profile;
    if (!profile) return;
    setPrimary(profile.protocol ?? 'auto');
    const desired = profile.enabledProtocols?.length
      ? profile.enabledProtocols
      : profile.activeProtocols?.length
        ? profile.activeProtocols
        : [connectionQuery.data?.protocol ?? 'vless'];
    setEnabledProtocols(ordered(desired));
  }, [connectionQuery.data]);

  useEffect(() => {
    setSaved(false);
    setDeploymentStage(null);
  }, [accountId]);

  const profile = connectionQuery.data?.profile;
  const activeProtocols = ordered(profile?.activeProtocols?.length
    ? profile.activeProtocols
    : connectionQuery.data?.protocol ? [connectionQuery.data.protocol] : []);
  const storedDesired = ordered(profile?.enabledProtocols?.length ? profile.enabledProtocols : activeProtocols);
  const currentPrimary = profile?.protocol ?? 'auto';
  const changed = primary !== currentPrimary || !sameProtocols(enabledProtocols, storedDesired);
  const activationPending = !sameProtocols(storedDesired, activeProtocols);
  const topologyBlocked = hysteria2Blocked && enabledProtocols.includes('hysteria2');
  const autoInvalid = primary === 'auto' && !enabledProtocols.includes(nodeDefault);
  const validationMessage = enabledProtocols.length === 0
    ? copy.selectOne
    : topologyBlocked
      ? copy.hysteria2Dedicated
      : autoInvalid
        ? copy.autoNeedsDefault
        : '';

  const saveMutation = useMutation({
    mutationFn: async () => {
      if (!profile) throw new Error(copy.loadError);
      if (!assignedServer) throw new Error(copy.noServer);
      if (validationMessage) throw new Error(validationMessage);

      const request: MultiProtocolUpdate = {
        name: profile.name,
        clientType: profile.clientType,
        deviceType: profile.deviceType,
        fingerprintMode: profile.fingerprintMode,
        fingerprint: profile.fingerprint,
        serverNameOverride: profile.serverNameOverride ?? '',
        spiderX: profile.spiderX || '/',
        mtu: profile.mtu ?? null,
        protocol: primary,
        enabledProtocols: ordered(enabledProtocols),
      };

      setDeploymentStage('saving_preference');
      await updateVpnAccountClientProfile(accountId, request);

      for (const protocol of ordered(enabledProtocols)) {
        await ensureProtocolRuntime(assignedServer, protocol, setDeploymentStage);
      }
      await deployPendingProtocol(assignedServer.id, setDeploymentStage);

      const connection = await getVpnAccountClientConnection(accountId) as MultiProtocolConnection;
      const activated = connection.profile.activeProtocols?.length
        ? connection.profile.activeProtocols
        : [connection.protocol];
      if (!sameProtocols(activated, enabledProtocols)) {
        throw new Error('protocol_set_activation_not_confirmed');
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
      setPrimary(connection.profile.protocol ?? 'auto');
      setEnabledProtocols(ordered(connection.profile.enabledProtocols ?? connection.profile.activeProtocols ?? [connection.protocol]));
      setDeploymentStage('completed');
      setSaved(true);
      window.setTimeout(() => setSaved(false), 2600);
    },
    onError: async () => {
      await queryClient.invalidateQueries({ queryKey });
    },
  });

  const toggleProtocol = (protocol: ClientProtocol) => {
    saveMutation.reset();
    setSaved(false);
    setDeploymentStage(null);
    setEnabledProtocols((current) => {
      const next = current.includes(protocol)
        ? current.filter((candidate) => candidate !== protocol)
        : [...current, protocol];
      const normalized = ordered(next);
      if (primary !== 'auto' && primary === protocol && !normalized.includes(protocol) && normalized.length > 0) {
        setPrimary(normalized[0]);
      }
      return normalized;
    });
  };

  const stageText = deploymentStage ? copy.stages[deploymentStage] : null;
  const errorDetail = mutationErrorDetail(saveMutation.error);
  const canRetry = !changed && activationPending;
  const activeSummary = useMemo(
    () => activeProtocols.length ? activeProtocols.map((protocol) => protocolLabel(protocol, copy)).join(' · ') : '—',
    [activeProtocols, copy],
  );

  return (
    <div className="panel feature-detail-panel vpn-account-protocol-preference-panel">
      <div className="panel-header">
        <div>
          <div className="panel-title">{copy.title}</div>
          <p className="panel-subtitle">{copy.subtitle}</p>
        </div>
        {connectionQuery.data && <span className="status-pill">{copy.active}: {activeSummary}</span>}
      </div>

      {connectionQuery.isLoading && <p className="empty-state">{copy.loading}</p>}
      {connectionQuery.isError && <div className="form-message form-message-error">{copy.loadError}</div>}

      {connectionQuery.data && (
        <>
          <div className="field">
            <span>{copy.enabled}</span>
            <div className="vpn-protocol-choice-grid">
              {protocolOrder.map((protocol) => {
                const disabled = saveMutation.isPending || (protocol === 'hysteria2' && hysteria2Blocked);
                return (
                  <label className="vpn-protocol-choice" key={protocol}>
                    <input
                      type="checkbox"
                      checked={enabledProtocols.includes(protocol)}
                      disabled={disabled}
                      onChange={() => toggleProtocol(protocol)}
                    />
                    <span>{protocolLabel(protocol, copy)}</span>
                  </label>
                );
              })}
            </div>
          </div>

          <div className="vpn-account-create-grid">
            <label className="field">
              <span>{copy.primary}</span>
              <select
                value={primary}
                disabled={saveMutation.isPending}
                onChange={(event) => {
                  setPrimary(event.target.value as ClientProtocolPreference);
                  saveMutation.reset();
                  setSaved(false);
                  setDeploymentStage(null);
                }}
              >
                <option value="auto">{copy.auto}</option>
                {enabledProtocols.map((protocol) => (
                  <option key={protocol} value={protocol}>{protocolLabel(protocol, copy)}</option>
                ))}
              </select>
              <span className="field-hint">{copy.primaryHint}</span>
            </label>
          </div>

          <div className="form-message">{copy.safety}</div>
          {activationPending && !saveMutation.isPending && <div className="form-message form-message-warning">{copy.pending}</div>}
          {validationMessage && <div className="form-message form-message-warning">{validationMessage}</div>}
          {changed && <div className="form-message">{copy.desired}: {enabledProtocols.map((protocol) => protocolLabel(protocol, copy)).join(' · ') || '—'}</div>}
          {saveMutation.isPending && stageText && <div className="form-message" role="status">{stageText}</div>}
          {saveMutation.isError && (
            <div className="form-message form-message-error">
              {copy.saveError}{errorDetail && <div>{copy.errorDetail}: {errorDetail}</div>}
            </div>
          )}
          {saved && <div className="form-message" role="status">{copy.saved}</div>}

          <div className="form-actions">
            <button
              className="primary-button"
              type="button"
              disabled={(!changed && !activationPending) || saveMutation.isPending || Boolean(validationMessage) || !assignedServer}
              onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending ? stageText ?? copy.saving : canRetry ? copy.retry : copy.save}
            </button>
          </div>
        </>
      )}
    </div>
  );
}