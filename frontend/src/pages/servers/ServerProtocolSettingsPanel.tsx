import { FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import {
  configureRecommendedProtocolSettings,
	configureRecommendedWireGuard,
  generateRealityKeypair,
  getProtocolSettings,
  updateProtocolSettings,
  type UpdateProtocolSettingsRequest,
} from '../../entities/server/api/serverApi';
import { getCurrentLocale, t } from '../../shared/i18n/i18n';

interface ProtocolSettingsFormState {
	protocol: 'vless' | 'wireguard';
  vlessPort: string;
  vlessFlow: string;
  vlessNetwork: string;
  realityPublicKey: string;
  realityShortId: string;
  realityServerName: string;
	wireGuardPort: string;
	wireGuardAddress: string;
	wireGuardDns: string;
}

const emptyFormState: ProtocolSettingsFormState = {
	protocol: 'vless',
  vlessPort: '',
  vlessFlow: '',
  vlessNetwork: '',
  realityPublicKey: '',
  realityShortId: '',
  realityServerName: '',
	wireGuardPort: '51820',
	wireGuardAddress: '10.66.0.1/24',
	wireGuardDns: '1.1.1.1',
};

function formatDate(value?: string | null): string {
  if (!value) {
    return '-';
  }

  const date = new Date(value);

  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function toFormState(settings: Awaited<ReturnType<typeof getProtocolSettings>>): ProtocolSettingsFormState {
  return {
		protocol: settings.protocol === 'wireguard' ? 'wireguard' : 'vless',
    vlessPort: String(settings.vless.port),
    vlessFlow: settings.vless.flow ?? '',
    vlessNetwork: settings.vless.network ?? '',
    realityPublicKey: settings.reality.publicKey ?? '',
    realityShortId: settings.reality.shortId ?? '',
    realityServerName: settings.reality.serverName ?? '',
		wireGuardPort: String(settings.wireGuard.port),
		wireGuardAddress: settings.wireGuard.address,
		wireGuardDns: settings.wireGuard.dns,
  };
}

function toRequest(
  form: ProtocolSettingsFormState,
  savedRealityPublicKey: string,
): UpdateProtocolSettingsRequest {
  const request: UpdateProtocolSettingsRequest = {
		protocol: form.protocol,
    vlessPort: Number(form.vlessPort),
    vlessFlow: form.vlessFlow.trim(),
    vlessNetwork: form.vlessNetwork.trim(),
    realityShortId: form.realityShortId.trim(),
    realityServerName: form.realityServerName.trim(),
		wireGuardPort: Number(form.wireGuardPort),
		wireGuardAddress: form.wireGuardAddress.trim(),
		wireGuardDns: form.wireGuardDns.trim(),
  };
  const realityPublicKey = form.realityPublicKey.trim();
  if (realityPublicKey !== savedRealityPublicKey.trim()) {
    request.realityPublicKey = realityPublicKey;
  }
  return request;
}

function getRecommendedCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      eyebrow: 'Рекомендуемая настройка',
      title: 'Настроить VLESS / Reality автоматически',
      description: 'RouteGate выберет безопасные параметры для All-in-One сервера, сгенерирует Reality keypair и Short ID и использует hostname сервера как SNI.',
      values: 'VLESS 8443 · TCP · XTLS Vision · Reality',
      portReason: 'HTTPS панели RouteGate остаётся на 443, поэтому VPN получает отдельный порт 8443 без конфликта с nginx.',
      action: 'Настроить автоматически',
		wireGuardAction: 'Настроить WireGuard',
		wireGuardPending: 'Настраиваем WireGuard…',
		protocol: 'Протокол VPN',
		wireGuardPort: 'Порт WireGuard',
		wireGuardAddress: 'Адрес интерфейса WireGuard',
		wireGuardDns: 'DNS для клиентов',
		wireGuardPublicKey: 'Публичный ключ сервера WireGuard',
		wireGuardProtocol: 'WireGuard',
      pending: 'Настраиваем…',
      configuredTitle: 'VLESS / Reality настроен',
      configuredDescription: 'Основные параметры готовы. Вернитесь в обзор, чтобы продолжить к созданию первого VPN-аккаунта.',
      continue: 'Продолжить →',
      error: 'Не удалось применить рекомендуемые настройки. Для автоматической настройки серверу нужен корректный hostname.',
      advanced: 'Расширенные настройки',
      advancedDescription: 'Изменяйте эти параметры только если вам нужен собственный порт, transport, flow или Reality SNI.',
      port443Warning: 'На All-in-One сервере порт 443 уже занят HTTPS-панелью RouteGate. Для VPN рекомендуется 8443.',
      incompleteReality: 'Для Reality заполните весь набор: публичный ключ, Short ID и имя сервера — либо очистите все три поля.',
    } as const;
  }

  return {
    eyebrow: 'Recommended setup',
    title: 'Configure VLESS / Reality automatically',
    description: 'RouteGate will choose safe All-in-One settings, generate the Reality keypair and Short ID, and use the server hostname as SNI.',
    values: 'VLESS 8443 · TCP · XTLS Vision · Reality',
    portReason: 'RouteGate HTTPS stays on 443, so VPN uses a separate 8443 port without conflicting with nginx.',
    action: 'Configure automatically',
		wireGuardAction: 'Configure WireGuard',
		wireGuardPending: 'Configuring WireGuard…',
		protocol: 'VPN protocol',
		wireGuardPort: 'WireGuard port',
		wireGuardAddress: 'WireGuard interface address',
		wireGuardDns: 'Client DNS',
		wireGuardPublicKey: 'WireGuard server public key',
		wireGuardProtocol: 'WireGuard',
    pending: 'Configuring…',
    configuredTitle: 'VLESS / Reality configured',
    configuredDescription: 'The required protocol settings are ready. Return to Overview to continue with the first VPN account.',
    continue: 'Continue →',
    error: 'Could not apply recommended settings. Automatic setup requires a valid server hostname.',
    advanced: 'Advanced settings',
    advancedDescription: 'Change these values only when you need a custom port, transport, flow, or Reality SNI.',
    port443Warning: 'On an All-in-One server, port 443 is already used by RouteGate HTTPS. Port 8443 is recommended for VPN.',
    incompleteReality: 'Reality requires the complete set: public key, Short ID, and server name — or all three fields must be empty.',
  } as const;
}

export function ServerProtocolSettingsPanel({ serverId }: { serverId: string }) {
  const queryClient = useQueryClient();
  const copy = getRecommendedCopy();
  const [form, setForm] = useState<ProtocolSettingsFormState>(emptyFormState);

  const settingsQuery = useQuery({
    queryKey: ['server-protocol-settings', serverId],
    queryFn: () => getProtocolSettings(serverId),
    enabled: Boolean(serverId),
  });

  useEffect(() => {
    if (settingsQuery.data) {
      setForm(toFormState(settingsQuery.data));
    }
  }, [settingsQuery.data]);

  const updateSettingsMutation = useMutation({
    mutationFn: (request: UpdateProtocolSettingsRequest) =>
      updateProtocolSettings(serverId, request),
    onSuccess: async (response) => {
      setForm(toFormState(response));
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
      setForm((current) => ({
        ...current,
        realityPublicKey: response.reality.publicKey ?? '',
      }));
    },
  });

  function updateField(field: keyof ProtocolSettingsFormState, value: string) {
    setForm((current) => ({
      ...current,
      [field]: value,
    }));
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    updateSettingsMutation.mutate(
      toRequest(form, settingsQuery.data?.reality.publicKey ?? ''),
    );
  }

  const portNumber = Number(form.vlessPort);
	const wireGuardPortNumber = Number(form.wireGuardPort);
  const realityValues = [form.realityPublicKey, form.realityShortId, form.realityServerName]
    .map((value) => value.trim());
  const realityTouched = realityValues.some(Boolean);
  const realityComplete = realityValues.every(Boolean);
	const protocolConfigured = form.protocol === 'wireguard'
		? Boolean(settingsQuery.data?.wireGuard.ready)
			&& Number.isInteger(wireGuardPortNumber)
			&& wireGuardPortNumber >= 1
			&& wireGuardPortNumber <= 65535
		: realityComplete
			&& Number.isInteger(portNumber)
			&& portNumber >= 1
			&& portNumber <= 65535;
  const mutationPending = updateSettingsMutation.isPending
    || realityKeypairMutation.isPending
		|| recommendedSettingsMutation.isPending
		|| wireGuardSettingsMutation.isPending;
  const canSave =
		(form.protocol === 'wireguard'
			? Number.isInteger(wireGuardPortNumber) && wireGuardPortNumber >= 1 && wireGuardPortNumber <= 65535
			: Number.isInteger(portNumber) && portNumber >= 1 && portNumber <= 65535 && (!realityTouched || realityComplete)) &&
    !mutationPending;
  const translatedKeypairActionLabel = form.realityPublicKey.trim() === ''
    ? t('protocolSettings.generateRealityKeypair')
    : t('protocolSettings.rotateRealityKeypair');

  return (
    <form className="panel protocol-settings-panel" onSubmit={handleSubmit}>
      <div className="panel-header">
        <div>
          <div className="panel-title">{t('protocolSettings.protocolSettingsTitle')}</div>
          <p className="panel-subtitle">{t('protocolSettings.protocolSettingsSubtitle')}</p>
        </div>
      </div>

      {settingsQuery.isLoading && <p className="empty-state">{t('protocolSettings.loading')}</p>}

      {settingsQuery.isError && (
        <div className="form-message form-message-error">{t('protocolSettings.protocolLoadError')}</div>
      )}

      {settingsQuery.data && (
        <div className={`protocol-recommended-setup${protocolConfigured ? ' protocol-recommended-setup-ready' : ''}`}>
          <div className="protocol-recommended-copy">
            <span className="protocol-recommended-eyebrow">{protocolConfigured ? copy.configuredTitle : copy.eyebrow}</span>
            <div className="protocol-recommended-title">{protocolConfigured ? copy.configuredTitle : copy.title}</div>
            <p>{protocolConfigured ? copy.configuredDescription : copy.description}</p>
            {!protocolConfigured && (
              <>
                <div className="protocol-recommended-values">{copy.values}</div>
                <p className="protocol-recommended-reason">{copy.portReason}</p>
              </>
            )}
          </div>
          <div className="protocol-recommended-actions">
            {protocolConfigured ? (
              <Link className="small-button" to="/">{copy.continue}</Link>
            ) : (
			  <>
				<button
				  className="primary-button"
				  type="button"
				  disabled={mutationPending}
				  onClick={() => recommendedSettingsMutation.mutate()}
				>
				  {recommendedSettingsMutation.isPending ? copy.pending : copy.action}
				</button>
				<button
				  className="small-button"
				  type="button"
				  disabled={mutationPending}
				  onClick={() => wireGuardSettingsMutation.mutate()}
				>
				  {wireGuardSettingsMutation.isPending ? copy.wireGuardPending : copy.wireGuardAction}
				</button>
			  </>
            )}
          </div>
        </div>
      )}

      {recommendedSettingsMutation.isError && (
        <div className="form-message form-message-error">{copy.error}</div>
      )}

      {recommendedSettingsMutation.isSuccess && (
        <div className="form-message">{t('protocolSettings.saved')}</div>
      )}

	  {wireGuardSettingsMutation.isError && (
		<div className="form-message form-message-error">{t('protocolSettings.protocolSaveError')}</div>
	  )}

	  {wireGuardSettingsMutation.isSuccess && (
		<div className="form-message">{t('protocolSettings.saved')}</div>
	  )}

      <details className="protocol-advanced-settings">
        <summary>{copy.advanced}</summary>
        <p className="panel-subtitle">{copy.advancedDescription}</p>

        {updateSettingsMutation.isError && (
          <div className="form-message form-message-error">
            {t('protocolSettings.protocolSaveError')}
          </div>
        )}

        {realityKeypairMutation.isError && (
          <div className="form-message form-message-error">{t('protocolSettings.keypairError')}</div>
        )}

        {updateSettingsMutation.isSuccess && (
          <div className="form-message">{t('protocolSettings.saved')}</div>
        )}

        {realityKeypairMutation.isSuccess && (
          <div className="form-message">{t('protocolSettings.keypairGenerated')}</div>
        )}

        {settingsQuery.data && (
          <>
            {portNumber === 443 && (
              <div className="protocol-port-warning">{copy.port443Warning}</div>
            )}
            {realityTouched && !realityComplete && (
              <div className="form-message form-message-error">{copy.incompleteReality}</div>
            )}

            <div className="protocol-settings-grid">
              <label className="field">
				<span>{copy.protocol}</span>
				<select value={form.protocol} onChange={(event) => updateField('protocol', event.target.value)}>
				  <option value="vless">VLESS / Reality</option>
				  <option value="wireguard">{copy.wireGuardProtocol}</option>
				</select>
              </label>
			  {form.protocol === 'wireguard' ? (
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

            <div className="protocol-advanced-actions">
			  {form.protocol === 'vless' && <button
                className="small-button"
                type="button"
                disabled={mutationPending || !settingsQuery.data}
                onClick={() => realityKeypairMutation.mutate()}
              >
                {realityKeypairMutation.isPending ? t('protocolSettings.generating') : translatedKeypairActionLabel}
			  </button>}
              <button className="small-button" type="submit" disabled={!canSave}>
                {updateSettingsMutation.isPending ? t('protocolSettings.saving') : t('protocolSettings.saveSettings')}
              </button>
            </div>

            <div className="protocol-settings-meta">
              <span>{t('protocolSettings.protocolValue', { value: settingsQuery.data.protocol })}</span>
              <span>{t('protocolSettings.realityValue', { value: settingsQuery.data.reality.enabled ? t('vpnAccounts.enabled') : t('vpnAccounts.disabled') })}</span>
              <span>{t('protocolSettings.privateKeyServerSide')}</span>
              <span>{t('protocolSettings.updatedValue', { value: formatDate(settingsQuery.data.updatedAt) })}</span>
            </div>
          </>
        )}
      </details>
    </form>
  );
}
