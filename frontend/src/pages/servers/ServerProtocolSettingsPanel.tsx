import { FormEvent, useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSearchParams } from 'react-router-dom';
import {
  configureRecommendedProtocolSettings,
  configureRecommendedWireGuard,
  generateRealityKeypair,
  getProtocolSettings,
  updateProtocolSettings,
  type UpdateProtocolSettingsRequest,
} from '../../entities/server/api/serverApi';
import { getCurrentLocale, t } from '../../shared/i18n/i18n';

type ManagedProtocol = 'vless' | 'wireguard' | 'hysteria2' | 'shadowsocks' | 'mtproto';

interface ProtocolSettingsFormState {
  vlessPort: string;
  vlessFlow: string;
  vlessNetwork: string;
  realityPublicKey: string;
  realityShortId: string;
  realityServerName: string;
  wireGuardPort: string;
  wireGuardAddress: string;
  wireGuardDns: string;
  hysteria2Port: string;
  hysteria2Domain: string;
  hysteria2AcmeEmail: string;
  hysteria2MasqueradeUrl: string;
  shadowsocksPort: string;
  mtprotoPort: string;
}

const emptyFormState: ProtocolSettingsFormState = {
  vlessPort: '',
  vlessFlow: '',
  vlessNetwork: '',
  realityPublicKey: '',
  realityShortId: '',
  realityServerName: '',
  wireGuardPort: '51820',
  wireGuardAddress: '10.66.0.1/24',
  wireGuardDns: '1.1.1.1',
  hysteria2Port: '443',
  hysteria2Domain: '',
  hysteria2AcmeEmail: '',
  hysteria2MasqueradeUrl: 'https://www.cloudflare.com/',
  shadowsocksPort: '8388',
  mtprotoPort: '8443',
};

function formatDate(value?: string | null): string {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function normalizeProtocol(value: string): ManagedProtocol {
  return value === 'wireguard'
    || value === 'hysteria2'
    || value === 'shadowsocks'
    || value === 'mtproto'
    ? value
    : 'vless';
}

function requestedProtocol(value: string | null): ManagedProtocol | null {
  if (value === 'vless'
    || value === 'wireguard'
    || value === 'hysteria2'
    || value === 'shadowsocks'
    || value === 'mtproto') {
    return value;
  }
  return null;
}

function toFormState(settings: Awaited<ReturnType<typeof getProtocolSettings>>): ProtocolSettingsFormState {
  return {
    vlessPort: String(settings.vless.port),
    vlessFlow: settings.vless.flow ?? '',
    vlessNetwork: settings.vless.network ?? '',
    realityPublicKey: settings.reality.publicKey ?? '',
    realityShortId: settings.reality.shortId ?? '',
    realityServerName: settings.reality.serverName ?? '',
    wireGuardPort: String(settings.wireGuard.port),
    wireGuardAddress: settings.wireGuard.address,
    wireGuardDns: settings.wireGuard.dns,
    hysteria2Port: String(settings.hysteria2.port),
    hysteria2Domain: settings.hysteria2.domain ?? '',
    hysteria2AcmeEmail: settings.hysteria2.acmeEmail ?? '',
    hysteria2MasqueradeUrl: settings.hysteria2.masqueradeUrl ?? 'https://www.cloudflare.com/',
    shadowsocksPort: String(settings.shadowsocks.port),
    mtprotoPort: String(settings.mtproto.port),
  };
}

function toRequest(
  form: ProtocolSettingsFormState,
  savedRealityPublicKey: string,
  editingProtocol: ManagedProtocol,
): UpdateProtocolSettingsRequest {
  const request: UpdateProtocolSettingsRequest = {};

  switch (editingProtocol) {
    case 'wireguard':
      request.wireGuardPort = Number(form.wireGuardPort);
      request.wireGuardAddress = form.wireGuardAddress.trim();
      request.wireGuardDns = form.wireGuardDns.trim();
      break;
    case 'hysteria2':
      request.hysteria2Port = Number(form.hysteria2Port);
      request.hysteria2Domain = form.hysteria2Domain.trim();
      request.hysteria2AcmeEmail = form.hysteria2AcmeEmail.trim();
      request.hysteria2MasqueradeUrl = form.hysteria2MasqueradeUrl.trim();
      break;
    case 'shadowsocks':
      request.shadowsocksPort = Number(form.shadowsocksPort);
      break;
    case 'mtproto':
      request.mtprotoPort = Number(form.mtprotoPort);
      break;
    default: {
      request.vlessPort = Number(form.vlessPort);
      request.vlessFlow = form.vlessFlow.trim();
      request.vlessNetwork = form.vlessNetwork.trim();
      request.realityShortId = form.realityShortId.trim();
      request.realityServerName = form.realityServerName.trim();
      const realityPublicKey = form.realityPublicKey.trim();
      if (realityPublicKey !== savedRealityPublicKey.trim()) {
        request.realityPublicKey = realityPublicKey;
      }
    }
  }
  return request;
}

function mutationErrorMessage(error: unknown, fallback: string): string {
  if (!(error instanceof Error) || error.message.trim() === '') return fallback;
  const reasonLabel = getCurrentLocale() === 'ru' ? 'Причина:' : 'Reason:';
  return `${fallback} ${reasonLabel} ${error.message.trim()}`;
}

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      recommendedEyebrow: 'Рекомендуемая настройка',
      configuredEyebrow: 'Настройки заполнены',
      saveDefault: 'Сделать основным',
      defaultSaved: 'Основной протокол изменён.',
      saveFirst: 'Сначала сохраните изменённые параметры протокола.',
      vlessTitle: 'Настроить VLESS / Reality автоматически',
      vlessDescription: 'RouteGate выберет безопасные параметры, сгенерирует Reality keypair и Short ID и использует hostname узла как SNI.',
      vlessValues: 'VLESS 8443 · TCP · XTLS Vision · Reality',
      vlessReason: 'HTTPS RouteGate остаётся на 443, поэтому для VPN рекомендуется отдельный порт 8443 без конфликта с nginx.',
      vlessAction: 'Настроить и сделать основным',
      wireGuardTitle: 'Настроить WireGuard автоматически',
      wireGuardDescription: 'RouteGate создаст серверную пару ключей WireGuard и применит безопасные значения интерфейса по умолчанию.',
      wireGuardValues: 'WireGuard · UDP 51820 · 10.66.0.1/24',
      wireGuardReason: 'Ключи и адреса клиентов останутся привязаны к VPN-аккаунтам, а серверный приватный ключ не показывается в интерфейсе.',
      wireGuardAction: 'Настроить и сделать основным',
      wireGuardPending: 'Настраиваем WireGuard…',
      manualDescription: 'Для этого протокола нет отдельной кнопки автонастройки. Заполните применимые поля ниже и сохраните настройки.',
      setupRequiredEyebrow: 'Требуется настройка',
      configuredDescription: 'Обязательные настройки заполнены. Протокол станет активным только после назначения VPN-аккаунту и успешного применения конфигурации Agent.',
      protocol: 'Основной протокол узла',
      editingProtocol: 'Протокол для настройки',
      editingProtocolHint: 'Настройте любой протокол независимо от основного. «Сохранить настройки» не меняет основной протокол.',
      wireGuardPort: 'Порт WireGuard',
      wireGuardAddress: 'Адрес интерфейса WireGuard',
      wireGuardDns: 'DNS для клиентов',
      wireGuardPublicKey: 'Публичный ключ сервера WireGuard',
      wireGuardProtocol: 'WireGuard',
      hysteria2Protocol: 'Hysteria2',
      hysteria2Port: 'UDP-порт Hysteria2',
      hysteria2Domain: 'Домен TLS / ACME',
      hysteria2AcmeEmail: 'Email для ACME',
      hysteria2MasqueradeUrl: 'HTTPS-сайт маскировки',
      hysteria2Hint: 'Домен должен указывать на этот VPN-узел. Hysteria получает и обновляет отдельный сертификат локально через ACME HTTP-01 (порт 80).',
      shadowsocksProtocol: 'Shadowsocks 2022',
      shadowsocksPort: 'TCP-порт Shadowsocks',
      shadowsocksHint: 'RouteGate использует фиксированный AEAD-2022 метод и отдельный ключ каждого аккаунта.',
      mtprotoProtocol: 'MTProto / FakeTLS',
      mtprotoPort: 'TCP-порт MTProto',
      mtprotoHint: 'FakeTLS-домен фиксирован: www.cloudflare.com. Секрет общий для узла и отображается только в защищённых данных доступа.',
      securityLabel: 'Защита',
      methodLabel: 'Метод',
      pending: 'Настраиваем…',
      error: 'Не удалось применить рекомендуемые настройки VLESS / Reality. Для автоматической настройки серверу нужен корректный hostname.',
      advanced: 'Параметры протокола',
      advancedDescription: 'Здесь отображаются только параметры, применимые к выбранному протоколу.',
      hysteriaHybridNotice: 'Для Hysteria2 на гибридном узле нужен отдельный домен с прямой DNS-записью на этот узел. TCP 443 остаётся у RouteGate, а UDP 443 используется Hysteria2.',
      port443Warning: 'На All-in-One сервере порт 443 уже занят HTTPS-панелью RouteGate. Для VLESS рекомендуется 8443.',
      incompleteReality: 'Для Reality заполните весь набор: публичный ключ, Short ID и имя сервера — либо очистите все три поля.',
    } as const;
  }

  return {
    recommendedEyebrow: 'Recommended setup',
    configuredEyebrow: 'Settings complete',
    saveDefault: 'Make default',
    defaultSaved: 'Node default protocol changed.',
    saveFirst: 'Save the changed protocol settings first.',
    vlessTitle: 'Configure VLESS / Reality automatically',
    vlessDescription: 'RouteGate will choose safe settings, generate the Reality keypair and Short ID, and use the node hostname as SNI.',
    vlessValues: 'VLESS 8443 · TCP · XTLS Vision · Reality',
    vlessReason: 'RouteGate HTTPS stays on 443, so VPN uses a separate 8443 port without conflicting with nginx.',
    vlessAction: 'Configure and make default',
    wireGuardTitle: 'Configure WireGuard automatically',
    wireGuardDescription: 'RouteGate will create the WireGuard server keypair and apply safe interface defaults.',
    wireGuardValues: 'WireGuard · UDP 51820 · 10.66.0.1/24',
    wireGuardReason: 'Client keys and addresses remain account-specific while the server private key stays hidden from the UI.',
    wireGuardAction: 'Configure and make default',
    wireGuardPending: 'Configuring WireGuard…',
    manualDescription: 'This protocol has no separate automatic-setup action. Complete the applicable fields below and save the settings.',
    setupRequiredEyebrow: 'Setup required',
    configuredDescription: 'Required settings are complete. The protocol becomes active only after assignment to a VPN account and a successful Agent config apply.',
    protocol: 'Node default protocol',
    editingProtocol: 'Protocol to configure',
    editingProtocolHint: 'Configure any protocol independently of the node default. Saving settings does not change the default.',
    wireGuardPort: 'WireGuard port',
    wireGuardAddress: 'WireGuard interface address',
    wireGuardDns: 'Client DNS',
    wireGuardPublicKey: 'WireGuard server public key',
    wireGuardProtocol: 'WireGuard',
    hysteria2Protocol: 'Hysteria2',
    hysteria2Port: 'Hysteria2 UDP port',
    hysteria2Domain: 'TLS / ACME domain',
    hysteria2AcmeEmail: 'ACME email',
    hysteria2MasqueradeUrl: 'Masquerade HTTPS site',
    hysteria2Hint: 'The domain must resolve to this VPN node. Hysteria obtains and renews a separate certificate locally through ACME HTTP-01 (port 80).',
    shadowsocksProtocol: 'Shadowsocks 2022',
    shadowsocksPort: 'Shadowsocks TCP port',
    shadowsocksHint: 'RouteGate uses a fixed AEAD-2022 method and a separate key for every account.',
    mtprotoProtocol: 'MTProto / FakeTLS',
    mtprotoPort: 'MTProto TCP port',
    mtprotoHint: 'The FakeTLS domain is fixed to www.cloudflare.com. The node-shared secret is exposed only through protected access data.',
    securityLabel: 'Security',
    methodLabel: 'Method',
    pending: 'Configuring…',
    error: 'Could not apply recommended VLESS / Reality settings. Automatic setup requires a valid server hostname.',
    advanced: 'Protocol parameters',
    advancedDescription: 'Only controls applicable to the selected protocol are shown here.',
    hysteriaHybridNotice: 'Hysteria2 on a Hybrid Node requires a dedicated hostname with a direct DNS record to this node. TCP 443 remains assigned to RouteGate, while Hysteria2 uses UDP 443.',
    port443Warning: 'On an All-in-One server, port 443 is already used by RouteGate HTTPS. Port 8443 is recommended for VLESS.',
    incompleteReality: 'Reality requires the complete set: public key, Short ID, and server name — or all three fields must be empty.',
  } as const;
}

function protocolLabel(protocol: ManagedProtocol, copy: ReturnType<typeof getCopy>): string {
  switch (protocol) {
    case 'wireguard': return copy.wireGuardProtocol;
    case 'hysteria2': return copy.hysteria2Protocol;
    case 'shadowsocks': return copy.shadowsocksProtocol;
    case 'mtproto': return copy.mtprotoProtocol;
    default: return 'VLESS / Reality';
  }
}

export function ServerProtocolSettingsPanel({
  serverId,
  deploymentRole,
}: {
  serverId: string;
  deploymentRole?: string;
}) {
  const queryClient = useQueryClient();
  const [searchParams] = useSearchParams();
  const copy = getCopy();
  const [form, setForm] = useState<ProtocolSettingsFormState>(emptyFormState);
  const [editingProtocol, setEditingProtocol] = useState<ManagedProtocol>('vless');
  const requested = requestedProtocol(searchParams.get('protocol'));
  const selectionScope = useRef('');

  const settingsQuery = useQuery({
    queryKey: ['server-protocol-settings', serverId],
    queryFn: () => getProtocolSettings(serverId),
    enabled: Boolean(serverId),
  });

  useEffect(() => {
    if (!settingsQuery.data) return;
    const next = toFormState(settingsQuery.data);
    const scope = `${serverId}:${requested ?? ''}`;
    if (selectionScope.current !== scope) {
      selectionScope.current = scope;
      setEditingProtocol(requested ?? normalizeProtocol(settingsQuery.data.protocol));
    }
    setForm(next);
  }, [serverId, requested, settingsQuery.data]);

  const updateSettingsMutation = useMutation({
    mutationFn: (request: UpdateProtocolSettingsRequest) => updateProtocolSettings(serverId, request),
    onSuccess: async (response) => {
      setForm(toFormState(response));
      await queryClient.invalidateQueries({ queryKey: ['server-protocol-settings', serverId] });
    },
  });

  const defaultProtocolMutation = useMutation({
    mutationFn: (protocol: ManagedProtocol) => updateProtocolSettings(serverId, { protocol }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['server-protocol-settings', serverId] });
    },
  });

  const recommendedSettingsMutation = useMutation({
    mutationFn: () => configureRecommendedProtocolSettings(serverId),
    onSuccess: async (response) => {
      setForm(toFormState(response));
      await queryClient.invalidateQueries({ queryKey: ['server-protocol-settings', serverId] });
    },
  });

  const wireGuardSettingsMutation = useMutation({
    mutationFn: () => configureRecommendedWireGuard(serverId),
    onSuccess: async (response) => {
      setForm(toFormState(response));
      await queryClient.invalidateQueries({ queryKey: ['server-protocol-settings', serverId] });
    },
  });

  const realityKeypairMutation = useMutation({
    mutationFn: () => generateRealityKeypair(serverId),
    onSuccess: (response) => {
      setForm((current) => ({ ...current, realityPublicKey: response.reality.publicKey ?? '' }));
    },
  });

  function updateField(field: keyof ProtocolSettingsFormState, value: string) {
    setForm((current) => ({ ...current, [field]: value }));
  }

  function selectProtocol(value: string) {
    setEditingProtocol(normalizeProtocol(value));
    defaultProtocolMutation.reset();
    updateSettingsMutation.reset();
    recommendedSettingsMutation.reset();
    wireGuardSettingsMutation.reset();
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    updateSettingsMutation.mutate(toRequest(
      form,
      settingsQuery.data?.reality.publicKey ?? '',
      editingProtocol,
    ));
  }

  const portNumber = Number(form.vlessPort);
  const wireGuardPortNumber = Number(form.wireGuardPort);
  const hysteria2PortNumber = Number(form.hysteria2Port);
  const shadowsocksPortNumber = Number(form.shadowsocksPort);
  const mtprotoPortNumber = Number(form.mtprotoPort);
  const realityValues = [form.realityPublicKey, form.realityShortId, form.realityServerName].map((value) => value.trim());
  const realityTouched = realityValues.some(Boolean);
  const realityComplete = realityValues.every(Boolean);
  const savedProtocol = normalizeProtocol(settingsQuery.data?.protocol ?? 'vless');
  const isDefault = editingProtocol === savedProtocol;
  const selectedSettingsDirty = settingsQuery.data ? JSON.stringify(toRequest(
    form,
    settingsQuery.data.reality.publicKey ?? '',
    editingProtocol,
  )) !== JSON.stringify(toRequest(
    toFormState(settingsQuery.data),
    settingsQuery.data.reality.publicKey ?? '',
    editingProtocol,
  )) : false;

  const selectedProtocolReady = editingProtocol === 'mtproto'
    ? Boolean(settingsQuery.data?.mtproto.ready) && Number.isInteger(mtprotoPortNumber) && mtprotoPortNumber >= 1 && mtprotoPortNumber <= 65535
    : editingProtocol === 'shadowsocks'
      ? Boolean(settingsQuery.data?.shadowsocks.ready) && Number.isInteger(shadowsocksPortNumber) && shadowsocksPortNumber >= 1 && shadowsocksPortNumber <= 65535
      : editingProtocol === 'hysteria2'
        ? Boolean(settingsQuery.data?.hysteria2.ready) && Number.isInteger(hysteria2PortNumber) && hysteria2PortNumber >= 1 && hysteria2PortNumber <= 65535
        : editingProtocol === 'wireguard'
          ? Boolean(settingsQuery.data?.wireGuard.ready) && Number.isInteger(wireGuardPortNumber) && wireGuardPortNumber >= 1 && wireGuardPortNumber <= 65535
          : realityComplete && Number.isInteger(portNumber) && portNumber >= 1 && portNumber <= 65535;

  const mutationPending = updateSettingsMutation.isPending
    || realityKeypairMutation.isPending
    || recommendedSettingsMutation.isPending
    || wireGuardSettingsMutation.isPending
    || defaultProtocolMutation.isPending;

  const canSave = (editingProtocol === 'mtproto'
    ? Number.isInteger(mtprotoPortNumber) && mtprotoPortNumber >= 1 && mtprotoPortNumber <= 65535
    : editingProtocol === 'shadowsocks'
      ? Number.isInteger(shadowsocksPortNumber) && shadowsocksPortNumber >= 1 && shadowsocksPortNumber <= 65535
      : editingProtocol === 'hysteria2'
        ? Number.isInteger(hysteria2PortNumber) && hysteria2PortNumber >= 1 && hysteria2PortNumber <= 65535
          && form.hysteria2Domain.trim() !== '' && form.hysteria2AcmeEmail.trim() !== ''
          && form.hysteria2MasqueradeUrl.trim() === 'https://www.cloudflare.com/'
        : editingProtocol === 'wireguard'
          ? Number.isInteger(wireGuardPortNumber) && wireGuardPortNumber >= 1 && wireGuardPortNumber <= 65535
          : Number.isInteger(portNumber) && portNumber >= 1 && portNumber <= 65535 && (!realityTouched || realityComplete))
    && !mutationPending;

  const translatedKeypairActionLabel = form.realityPublicKey.trim() === ''
    ? t('protocolSettings.generateRealityKeypair')
    : t('protocolSettings.rotateRealityKeypair');
  const selectedProtocolLabel = protocolLabel(editingProtocol, copy);
  const isVless = editingProtocol === 'vless';
  const isWireGuard = editingProtocol === 'wireguard';
  const showRecommendedSetup = !selectedProtocolReady;

  const recommendedTitle = isVless ? copy.vlessTitle : isWireGuard ? copy.wireGuardTitle : selectedProtocolLabel;
  const recommendedDescription = isVless ? copy.vlessDescription : isWireGuard ? copy.wireGuardDescription : copy.manualDescription;
  const recommendedValues = isVless ? copy.vlessValues : isWireGuard ? copy.wireGuardValues : null;
  const recommendedReason = isVless ? copy.vlessReason : isWireGuard ? copy.wireGuardReason : null;

  return (
    <form className="panel protocol-settings-panel" onSubmit={handleSubmit}>
      <div className="panel-header">
        <div>
          <div className="panel-title">{t('protocolSettings.protocolSettingsTitle')}</div>
          <p className="panel-subtitle">{t('protocolSettings.protocolSettingsSubtitle')}</p>
        </div>
      </div>

      {settingsQuery.isLoading && <p className="empty-state">{t('protocolSettings.loading')}</p>}
      {settingsQuery.isError && <div className="form-message form-message-error">{t('protocolSettings.protocolLoadError')}</div>}

      {settingsQuery.data && (
        <section className="protocol-advanced-settings">
          <h3>{copy.advanced}</h3>
          <p className="panel-subtitle">{copy.advancedDescription}</p>
          <div className="protocol-choice">
            <label className="field protocol-editing-selector">
              <span>{copy.editingProtocol}{isDefault && <span className="protocol-default-badge">{t('protocolSettings.default')}</span>}</span>
              <select value={editingProtocol} onChange={(event) => selectProtocol(event.target.value)}>
                <option value="vless">VLESS / Reality</option>
                <option value="wireguard">{copy.wireGuardProtocol}</option>
                <option value="hysteria2">{copy.hysteria2Protocol}</option>
                <option value="shadowsocks">{copy.shadowsocksProtocol}</option>
                <option value="mtproto">{copy.mtprotoProtocol}</option>
              </select>
              <small>{copy.editingProtocolHint}</small>
            </label>
            <div className="protocol-current-default">{copy.protocol}: <strong>{protocolLabel(savedProtocol, copy)}</strong></div>
            {!isDefault && selectedProtocolReady && (
              <>
                <button className="small-button" type="button" disabled={mutationPending || selectedSettingsDirty} onClick={() => defaultProtocolMutation.mutate(editingProtocol)}>
                  {defaultProtocolMutation.isPending ? t('protocolSettings.saving') : copy.saveDefault}
                </button>
                {selectedSettingsDirty && <small>{copy.saveFirst}</small>}
              </>
            )}
            {defaultProtocolMutation.isError && <div className="form-message form-message-error">{mutationErrorMessage(defaultProtocolMutation.error, t('protocolSettings.protocolSaveError'))}</div>}
            {defaultProtocolMutation.isSuccess && isDefault && <div className="form-message">{copy.defaultSaved}</div>}
          </div>

          {showRecommendedSetup && (
            <div className="protocol-recommended-setup">
              <div className="protocol-recommended-copy">
                <span className="protocol-recommended-eyebrow">{isVless || isWireGuard ? copy.recommendedEyebrow : copy.setupRequiredEyebrow}</span>
                <div className="protocol-recommended-title">{recommendedTitle}</div>
                <p>{recommendedDescription}</p>
                {!selectedProtocolReady && recommendedValues && <div className="protocol-recommended-values">{recommendedValues}</div>}
                {!selectedProtocolReady && recommendedReason && <p className="protocol-recommended-reason">{recommendedReason}</p>}
              </div>
              <div className="protocol-recommended-actions">
                {isVless ? (
                  <button
                    className="primary-button"
                    type="button"
                    disabled={mutationPending}
                    onClick={() => recommendedSettingsMutation.mutate()}
                  >
                    {recommendedSettingsMutation.isPending ? copy.pending : copy.vlessAction}
                  </button>
                ) : isWireGuard ? (
                  <button
                    className="primary-button"
                    type="button"
                    disabled={mutationPending}
                    onClick={() => wireGuardSettingsMutation.mutate()}
                  >
                    {wireGuardSettingsMutation.isPending ? copy.wireGuardPending : copy.wireGuardAction}
                  </button>
                ) : null}
              </div>
            </div>
          )}

          {recommendedSettingsMutation.isError && <div className="form-message form-message-error">{copy.error}</div>}
          {recommendedSettingsMutation.isSuccess && <div className="form-message">{t('protocolSettings.saved')}</div>}
          {wireGuardSettingsMutation.isError && <div className="form-message form-message-error">{t('protocolSettings.protocolSaveError')}</div>}
          {wireGuardSettingsMutation.isSuccess && <div className="form-message">{t('protocolSettings.saved')}</div>}

        {updateSettingsMutation.isError && (
          <div className="form-message form-message-error protocol-settings-feedback">
            {mutationErrorMessage(updateSettingsMutation.error, t('protocolSettings.protocolSaveError'))}
          </div>
        )}
        {isVless && realityKeypairMutation.isError && <div className="form-message form-message-error">{t('protocolSettings.keypairError')}</div>}
        {updateSettingsMutation.isSuccess && <div className="form-message">{t('protocolSettings.saved')}</div>}
        {isVless && realityKeypairMutation.isSuccess && <div className="form-message">{t('protocolSettings.keypairGenerated')}</div>}

        {settingsQuery.data && (
          <>
            {isVless && portNumber === 443 && <div className="protocol-port-warning">{copy.port443Warning}</div>}
            {isVless && realityTouched && !realityComplete && <div className="form-message form-message-error">{copy.incompleteReality}</div>}
            {editingProtocol === 'hysteria2' && deploymentRole === 'hybrid' && (
              <div className="protocol-port-warning">{copy.hysteriaHybridNotice}</div>
            )}

            <div className="protocol-settings-grid">
              {editingProtocol === 'mtproto' ? (
                <>
                  <label className="field">
                    <span>{copy.mtprotoPort}</span>
                    <input inputMode="numeric" min="1" max="65535" type="number" value={form.mtprotoPort} onChange={(event) => updateField('mtprotoPort', event.target.value)} />
                    <small>{copy.mtprotoHint}</small>
                  </label>
                  <label className="field"><span>{copy.securityLabel}</span><input value={settingsQuery.data.mtproto.frontingDomain} readOnly /></label>
                </>
              ) : editingProtocol === 'shadowsocks' ? (
                <>
                  <label className="field">
                    <span>{copy.shadowsocksPort}</span>
                    <input inputMode="numeric" min="1" max="65535" type="number" value={form.shadowsocksPort} onChange={(event) => updateField('shadowsocksPort', event.target.value)} />
                    <small>{copy.shadowsocksHint}</small>
                  </label>
                  <label className="field"><span>{copy.methodLabel}</span><input value={settingsQuery.data.shadowsocks.method} readOnly /></label>
                </>
              ) : editingProtocol === 'hysteria2' ? (
                <>
                  <label className="field"><span>{copy.hysteria2Port}</span><input inputMode="numeric" min="1" max="65535" type="number" value={form.hysteria2Port} onChange={(event) => updateField('hysteria2Port', event.target.value)} /></label>
                  <label className="field"><span>{copy.hysteria2Domain}</span><input value={form.hysteria2Domain} onChange={(event) => updateField('hysteria2Domain', event.target.value)} /></label>
                  <label className="field"><span>{copy.hysteria2AcmeEmail}</span><input type="email" value={form.hysteria2AcmeEmail} onChange={(event) => updateField('hysteria2AcmeEmail', event.target.value)} /></label>
                  <label className="field"><span>{copy.hysteria2MasqueradeUrl}</span><input type="url" value={form.hysteria2MasqueradeUrl} readOnly /></label>
                </>
              ) : editingProtocol === 'wireguard' ? (
                <>
                  <label className="field"><span>{copy.wireGuardPort}</span><input inputMode="numeric" min="1" max="65535" type="number" value={form.wireGuardPort} onChange={(event) => updateField('wireGuardPort', event.target.value)} /></label>
                  <label className="field"><span>{copy.wireGuardAddress}</span><input value={form.wireGuardAddress} onChange={(event) => updateField('wireGuardAddress', event.target.value)} /></label>
                  <label className="field"><span>{copy.wireGuardDns}</span><input value={form.wireGuardDns} onChange={(event) => updateField('wireGuardDns', event.target.value)} /></label>
                  <label className="field"><span>{copy.wireGuardPublicKey}</span><input value={settingsQuery.data.wireGuard.publicKey ?? ''} readOnly /></label>
                </>
              ) : (
                <>
                  <label className="field"><span>{t('protocolSettings.vlessPort')}</span><input inputMode="numeric" min="1" max="65535" type="number" value={form.vlessPort} onChange={(event) => updateField('vlessPort', event.target.value)} /></label>
                  <label className="field"><span>{t('protocolSettings.vlessFlow')}</span><select value={form.vlessFlow} onChange={(event) => updateField('vlessFlow', event.target.value)}><option value="">{t('protocolSettings.default')}</option><option value="xtls-rprx-vision">xtls-rprx-vision</option></select></label>
                  <label className="field"><span>{t('protocolSettings.vlessNetwork')}</span><select value={form.vlessNetwork} onChange={(event) => updateField('vlessNetwork', event.target.value)}><option value="">{t('protocolSettings.default')}</option><option value="tcp">tcp</option><option value="ws">ws</option><option value="grpc">grpc</option><option value="http">http</option></select></label>
                  <label className="field"><span>{t('protocolSettings.realityPublicKey')}</span><input value={form.realityPublicKey} onChange={(event) => updateField('realityPublicKey', event.target.value)} /></label>
                  <label className="field"><span>{t('protocolSettings.realityShortId')}</span><input value={form.realityShortId} onChange={(event) => updateField('realityShortId', event.target.value)} /></label>
                  <label className="field"><span>{t('protocolSettings.realityServerName')}</span><input value={form.realityServerName} onChange={(event) => updateField('realityServerName', event.target.value)} /></label>
                </>
              )}
            </div>

            {editingProtocol === 'hysteria2' && <p className="protocol-settings-hint">{copy.hysteria2Hint}</p>}

            <div className="protocol-advanced-actions">
              {isVless && (
                <button
                  className="small-button"
                  type="button"
                  disabled={mutationPending || !settingsQuery.data}
                  onClick={() => realityKeypairMutation.mutate()}
                >
                  {realityKeypairMutation.isPending ? t('protocolSettings.generating') : translatedKeypairActionLabel}
                </button>
              )}
              <button className="primary-button" type="submit" disabled={!canSave}>
                {updateSettingsMutation.isPending ? t('protocolSettings.saving') : t('protocolSettings.saveSettings')}
              </button>
            </div>

            <div className="protocol-settings-meta">
              <span>{t('protocolSettings.protocolValue', { value: selectedProtocolLabel })}</span>
              {isVless && <span>{t('protocolSettings.realityValue', { value: settingsQuery.data.reality.enabled ? t('vpnAccounts.enabled') : t('vpnAccounts.disabled') })}</span>}
              {isVless && <span>{t('protocolSettings.privateKeyServerSide')}</span>}
              <span>{t('protocolSettings.updatedValue', { value: formatDate(settingsQuery.data.updatedAt) })}</span>
            </div>
          </>
        )}
        </section>
      )}
    </form>
  );
}
