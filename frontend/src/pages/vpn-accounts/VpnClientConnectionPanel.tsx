import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createVpnAccountSubscriptionToken,
  getVpnAccountClientConnection,
  rotateVpnAccountSubscriptionToken,
  updateVpnAccountClientProfile,
  type ClientFingerprintMode,
  type SubscriptionTokenResponse,
  type UpdateVpnClientProfileRequest,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { getCurrentLocale, t } from '../../shared/i18n/i18n';
import { ShareAccessActions } from '../../shared/ui/ShareAccessActions';
import { SubscriptionQrDialog } from '../../shared/ui/SubscriptionQrDialog';
import './vpn-client-connection.css';

type VpnClientConnectionPanelProps = {
  accountId: string;
};

const fingerprintOptions = ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', 'random', 'randomized'];

function formatDate(value?: string | null): string {
  if (!value) {
    return t('common.notAvailable');
  }
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function getErrorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() !== '' ? error.message : fallback;
}

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      title: 'Подключение VPN-клиента',
      subtitle: 'Постоянный профиль VLESS Reality сохраняется в RouteGate и не меняется после повторного входа.',
      loading: 'Загрузка клиентского профиля...',
      loadError: 'Не удалось загрузить клиентский профиль.',
      ready: 'Готово к подключению',
      readyDescription: 'QR-код и VLESS-ссылка используют сохранённые параметры этого профиля.',
      format: 'VLESS Reality',
      showQr: 'Показать QR',
      copyVless: 'Скопировать VLESS-ссылку',
		copyWireGuard: 'Скопировать конфиг WireGuard',
		wireGuardFormat: 'WireGuard',
		wireGuardReadyDescription: 'QR-код содержит готовый конфиг WireGuard с приватным ключом этого профиля.',
		wireGuardConfig: 'Конфиг WireGuard',
		hysteria2Format: 'Hysteria2',
		copyHysteria2: 'Скопировать Hysteria2 URI',
		hysteria2ReadyDescription: 'QR-код содержит стандартный hysteria2:// URI с логином, паролем и проверяемым TLS SNI.',
		hysteria2Uri: 'Hysteria2 URI',
      copy: 'Копировать',
      copied: 'Скопировано',
      credentialWarning: 'QR-код и VLESS-ссылка предоставляют доступ к VPN. Не публикуйте их.',
      profileSettings: 'Настройки клиентского профиля',
      profileName: 'Название профиля',
      clientType: 'VPN-клиент',
      deviceType: 'Устройство',
      other: 'Другое',
      fingerprintMode: 'Режим fingerprint',
      auto: 'Auto — рекомендуется',
      manual: 'Вручную',
      fingerprint: 'TLS fingerprint',
      resolvedFingerprint: 'Фактически выбран',
      autoHint: 'Auto сохраняет совместимый вариант. Сейчас RouteGate выбирает Firefox как безопасный профиль совместимости.',
      endpoint: 'Endpoint',
      serverName: 'Reality SNI',
      network: 'Transport',
      flow: 'Flow',
      serverNameOverride: 'Переопределить SNI',
      serverNameHint: 'Оставьте пустым, чтобы использовать настройку сервера.',
      spiderX: 'SpiderX',
      mtu: 'MTU',
      autoPlaceholder: 'Авто',
      mtuHint: 'Пусто — Auto. Сторонний клиент может игнорировать MTU из профиля.',
      save: 'Сохранить профиль',
      saving: 'Сохранение...',
      saved: 'Профиль сохранён',
      saveError: 'Не удалось сохранить клиентский профиль.',
      advancedSubscription: 'Расширенный URL подписки',
      subscriptionDescription: 'Отдельный токен для внутреннего формата RouteGate. Он не нужен для прямого QR-кода.',
      createSubscription: 'Создать URL подписки',
      rotateSubscription: 'Обновить URL подписки',
      subscriptionBusy: 'Подготовка...',
      subscriptionError: 'Не удалось создать URL подписки.',
      subscriptionUrl: 'URL подписки RouteGate',
      expires: 'Истекает',
      qrTitle: 'QR-код для VPN-клиента',
      vlessLink: 'VLESS-ссылка',
      close: 'Закрыть',
    } as const;
  }

  return {
    title: 'Connect VPN client',
    subtitle: 'The persistent VLESS Reality profile is stored in RouteGate and remains stable after signing in again.',
    loading: 'Loading client profile...',
    loadError: 'Could not load the client profile.',
    ready: 'Ready to connect',
    readyDescription: 'The QR code and VLESS link use the saved settings of this profile.',
    format: 'VLESS Reality',
    showQr: 'Show QR',
    copyVless: 'Copy VLESS link',
		copyWireGuard: 'Copy WireGuard config',
		wireGuardFormat: 'WireGuard',
		wireGuardReadyDescription: 'The QR code contains a ready-to-import WireGuard config with this profile’s private key.',
		wireGuardConfig: 'WireGuard config',
		hysteria2Format: 'Hysteria2',
		copyHysteria2: 'Copy Hysteria2 URI',
		hysteria2ReadyDescription: 'The QR code contains a standard hysteria2:// URI with userpass credentials and verified TLS SNI.',
		hysteria2Uri: 'Hysteria2 URI',
    copy: 'Copy',
    copied: 'Copied',
    credentialWarning: 'The QR code and VLESS link grant VPN access. Do not publish them.',
    profileSettings: 'Client profile settings',
    profileName: 'Profile name',
    clientType: 'VPN client',
    deviceType: 'Device',
    other: 'Other',
    fingerprintMode: 'Fingerprint mode',
    auto: 'Auto — recommended',
    manual: 'Manual',
    fingerprint: 'TLS fingerprint',
    resolvedFingerprint: 'Resolved value',
    autoHint: 'Auto stores a compatible option. RouteGate currently selects Firefox as the compatibility-safe profile.',
    endpoint: 'Endpoint',
    serverName: 'Reality SNI',
    network: 'Transport',
    flow: 'Flow',
    serverNameOverride: 'Override SNI',
    serverNameHint: 'Leave empty to inherit the server setting.',
    spiderX: 'SpiderX',
    mtu: 'MTU',
    autoPlaceholder: 'Auto',
    mtuHint: 'Blank means Auto. A third-party client may ignore profile MTU.',
    save: 'Save profile',
    saving: 'Saving...',
    saved: 'Profile saved',
    saveError: 'Could not save client profile.',
    advancedSubscription: 'Advanced subscription URL',
    subscriptionDescription: 'A separate token for RouteGate’s internal subscription format. Direct QR does not require it.',
    createSubscription: 'Create subscription URL',
    rotateSubscription: 'Refresh subscription URL',
    subscriptionBusy: 'Preparing...',
    subscriptionError: 'Could not create subscription URL.',
    subscriptionUrl: 'RouteGate subscription URL',
    expires: 'Expires',
    qrTitle: 'QR code for VPN client',
    vlessLink: 'VLESS link',
    close: 'Close',
  } as const;
}

export function VpnClientConnectionPanel({ accountId }: VpnClientConnectionPanelProps) {
  const copy = getCopy();
  const queryClient = useQueryClient();
  const queryKey = ['vpn-account-client-connection', accountId] as const;
  const [copiedTarget, setCopiedTarget] = useState<string | null>(null);
  const [isQrOpen, setIsQrOpen] = useState(false);
  const [subscriptionToken, setSubscriptionToken] = useState<SubscriptionTokenResponse | null>(null);
  const [saved, setSaved] = useState(false);
  const [profileName, setProfileName] = useState('Default');
  const [clientType, setClientType] = useState('other');
  const [deviceType, setDeviceType] = useState('other');
  const [fingerprintMode, setFingerprintMode] = useState<ClientFingerprintMode>('auto');
  const [fingerprint, setFingerprint] = useState('firefox');
  const [serverNameOverride, setServerNameOverride] = useState('');
  const [spiderX, setSpiderX] = useState('/');
  const [mtu, setMtu] = useState('');

  const connectionQuery = useQuery({
    queryKey,
    queryFn: () => getVpnAccountClientConnection(accountId),
  });

  useEffect(() => {
    const profile = connectionQuery.data?.profile;
    if (!profile) {
      return;
    }
    setProfileName(profile.name);
    setClientType(profile.clientType);
    setDeviceType(profile.deviceType);
    setFingerprintMode(profile.fingerprintMode);
    setFingerprint(profile.fingerprint);
    setServerNameOverride(profile.serverNameOverride ?? '');
    setSpiderX(profile.spiderX || '/');
    setMtu(profile.mtu ? String(profile.mtu) : '');
  }, [connectionQuery.data]);

  useEffect(() => {
    setSubscriptionToken(null);
    setCopiedTarget(null);
    setIsQrOpen(false);
    setSaved(false);
  }, [accountId]);

  const saveMutation = useMutation({
    mutationFn: () => {
      const request: UpdateVpnClientProfileRequest = {
        name: profileName.trim() || 'Default',
        clientType,
        deviceType,
        fingerprintMode,
        fingerprint,
        serverNameOverride: serverNameOverride.trim(),
        spiderX: spiderX.trim() || '/',
        mtu: mtu.trim() === '' ? null : Number(mtu),
      };
      return updateVpnAccountClientProfile(accountId, request);
    },
    onMutate: () => setSaved(false),
    onSuccess: (connection) => {
      queryClient.setQueryData(queryKey, connection);
      setSaved(true);
      window.setTimeout(() => setSaved(false), 2200);
    },
  });

  const subscriptionMutation = useMutation({
    mutationFn: () => subscriptionToken
      ? rotateVpnAccountSubscriptionToken(accountId)
      : createVpnAccountSubscriptionToken(accountId),
    onSuccess: setSubscriptionToken,
  });

  const copyToClipboard = async (target: string, value: string) => {
    if (!navigator.clipboard || value.trim() === '') {
      return;
    }
    await navigator.clipboard.writeText(value);
    setCopiedTarget(target);
    window.setTimeout(() => setCopiedTarget(null), 1800);
  };

  const connection = connectionQuery.data;
	const isWireGuard = connection?.format === 'wireguard-config';
	const isHysteria2 = connection?.format === 'hysteria2-uri';
	const connectionText = connection?.wireGuardConfig ?? connection?.hysteria2Uri ?? connection?.vlessLink ?? '';

  return (
    <div className="panel subscription-panel feature-detail-panel vpn-client-connection-panel">
      <div className="panel-header">
        <div>
          <div className="panel-title">{copy.title}</div>
          <p className="panel-subtitle">{copy.subtitle}</p>
        </div>
      </div>

      {connectionQuery.isLoading && <p className="empty-state">{copy.loading}</p>}
      {connectionQuery.isError && (
        <div className="form-message form-message-error">
          {getErrorMessage(connectionQuery.error, copy.loadError)}
        </div>
      )}

      {connection && (
        <div className="subscription-result subscription-self-service-card">
          <div className="form-message form-message-warning">{copy.credentialWarning}</div>

          <div className="subscription-url-stack vpn-client-primary-card">
            <div className="subscription-url-header">
              <div className="subscription-url-meta">
                <div className="subscription-url-label">{copy.ready}</div>
				<p className="subscription-url-helper">{isWireGuard ? copy.wireGuardReadyDescription : isHysteria2 ? copy.hysteria2ReadyDescription : copy.readyDescription}</p>
              </div>
			  <span className="vpn-client-format-badge">{isWireGuard ? copy.wireGuardFormat : isHysteria2 ? copy.hysteria2Format : copy.format}</span>
            </div>

            <div className="vpn-client-primary-actions">
              <button className="primary-button" type="button" onClick={() => setIsQrOpen(true)}>
                {copy.showQr}
              </button>
              <button
                className="small-button"
                type="button"
				onClick={() => void copyToClipboard('connection-config', connectionText)}
              >
				{copiedTarget === 'connection-config' ? copy.copied : (isWireGuard ? copy.copyWireGuard : isHysteria2 ? copy.copyHysteria2 : copy.copyVless)}
              </button>
			  {!isWireGuard && !isHysteria2 && (
				<ShareAccessActions
				  vlessLink={connection.vlessLink ?? ''}
				  profileName={connection.profile.name}
				  includeQrShare
				  compact
				/>
			  )}
            </div>

            <div className="vpn-client-runtime-grid">
              <div><span>{copy.endpoint}</span><strong>{connection.endpoint}</strong></div>
			  {!isWireGuard && <div><span>{copy.serverName}</span><strong>{connection.serverName}</strong></div>}
			  {!isWireGuard && <div><span>{copy.network}</span><strong>{connection.network}</strong></div>}
			  {!isWireGuard && !isHysteria2 && <div><span>{copy.flow}</span><strong>{connection.flow || '—'}</strong></div>}
              <div><span>{copy.resolvedFingerprint}</span><strong>{connection.profile.resolvedFingerprint}</strong></div>
            </div>
          </div>

          <details className="vpn-client-advanced feature-subpanel" open>
            <summary>{copy.profileSettings}</summary>
            <div className="vpn-client-advanced-content">
              <div className="vpn-client-settings-grid">
                <label className="field">
                  <span>{copy.profileName}</span>
                  <input value={profileName} onChange={(event) => setProfileName(event.target.value)} />
                </label>
                <label className="field">
                  <span>{copy.clientType}</span>
                  <select value={clientType} onChange={(event) => setClientType(event.target.value)}>
                    <option value="v2rayn">v2rayN</option>
                    <option value="v2raytun">V2RayTun</option>
                    <option value="v2box">V2Box</option>
                    <option value="sing-box">sing-box</option>
                    <option value="other">{copy.other}</option>
                  </select>
                </label>
                <label className="field">
                  <span>{copy.deviceType}</span>
                  <select value={deviceType} onChange={(event) => setDeviceType(event.target.value)}>
                    <option value="windows">Windows</option>
                    <option value="ios">iOS</option>
                    <option value="android">Android</option>
                    <option value="macos">macOS</option>
                    <option value="linux">Linux</option>
                    <option value="other">{copy.other}</option>
                  </select>
                </label>
                <label className="field">
                  <span>{copy.fingerprintMode}</span>
                  <select
                    value={fingerprintMode}
                    onChange={(event) => setFingerprintMode(event.target.value as ClientFingerprintMode)}
                  >
                    <option value="auto">{copy.auto}</option>
                    <option value="manual">{copy.manual}</option>
                  </select>
                </label>
                <label className="field">
                  <span>{copy.fingerprint}</span>
                  <select
                    value={fingerprint}
                    disabled={fingerprintMode === 'auto'}
                    onChange={(event) => setFingerprint(event.target.value)}
                  >
                    {fingerprintOptions.map((option) => <option value={option} key={option}>{option}</option>)}
                  </select>
                  {fingerprintMode === 'auto' && <small>{copy.autoHint}</small>}
                </label>
                <label className="field">
                  <span>{copy.serverNameOverride}</span>
                  <input
                    value={serverNameOverride}
                    placeholder={connection.serverName}
                    onChange={(event) => setServerNameOverride(event.target.value)}
                  />
                  <small>{copy.serverNameHint}</small>
                </label>
                <label className="field">
                  <span>{copy.spiderX}</span>
                  <input value={spiderX} onChange={(event) => setSpiderX(event.target.value)} />
                </label>
                <label className="field">
                  <span>{copy.mtu}</span>
                  <input
                    type="number"
                    min="576"
                    max="9000"
                    value={mtu}
                    placeholder={copy.autoPlaceholder}
                    onChange={(event) => setMtu(event.target.value)}
                  />
                  <small>{copy.mtuHint}</small>
                </label>
              </div>

              {saveMutation.isError && (
                <div className="form-message form-message-error">
                  {getErrorMessage(saveMutation.error, copy.saveError)}
                </div>
              )}
              {saved && <div className="form-message form-message-success">{copy.saved}</div>}

              <div className="form-actions">
                <button
                  className="primary-button"
                  type="button"
                  disabled={saveMutation.isPending}
                  onClick={() => saveMutation.mutate()}
                >
                  {saveMutation.isPending ? copy.saving : copy.save}
                </button>
              </div>
            </div>
          </details>

          <details className="vpn-client-advanced feature-subpanel">
            <summary>{copy.advancedSubscription}</summary>
            <div className="vpn-client-advanced-content">
              <div className="form-message form-message-warning">{copy.subscriptionDescription}</div>
              <button
                className="small-button"
                type="button"
                disabled={subscriptionMutation.isPending}
                onClick={() => subscriptionMutation.mutate()}
              >
                {subscriptionMutation.isPending
                  ? copy.subscriptionBusy
                  : subscriptionToken
                    ? copy.rotateSubscription
                    : copy.createSubscription}
              </button>

              {subscriptionMutation.isError && (
                <div className="form-message form-message-error">{copy.subscriptionError}</div>
              )}

              {subscriptionToken && (
                <div className="subscription-url-stack">
                  <div className="subscription-url-header">
                    <div className="subscription-url-meta">
                      <div className="subscription-url-label">{copy.subscriptionUrl}</div>
                      <p className="subscription-url-helper">{copy.expires}: {formatDate(subscriptionToken.expiresAt)}</p>
                    </div>
                    <button
                      className="small-button"
                      type="button"
                      onClick={() => void copyToClipboard('subscription-url', subscriptionToken.subscriptionUrl)}
                    >
                      {copiedTarget === 'subscription-url' ? copy.copied : copy.copy}
                    </button>
                  </div>
                  <code className="subscription-url-value">{subscriptionToken.subscriptionUrl}</code>
                </div>
              )}
            </div>
          </details>
        </div>
      )}

      <SubscriptionQrDialog
        isOpen={isQrOpen}
        title={copy.qrTitle}
        onClose={() => setIsQrOpen(false)}
		qrText={connectionText}
        qrTitle={copy.qrTitle}
        qrSubtitle={connection?.profile.resolvedFingerprint ?? copy.format}
		url={connectionText}
		urlLabel={isWireGuard ? copy.wireGuardConfig : isHysteria2 ? copy.hysteria2Uri : copy.vlessLink}
		onCopyQrText={() => void copyToClipboard('qr-connection', connectionText)}
		copyQrLabel={isWireGuard ? copy.copyWireGuard : isHysteria2 ? copy.copyHysteria2 : copy.copyVless}
        copyCopiedLabel={copy.copied}
		copied={copiedTarget === 'qr-connection'}
        closeLabel={copy.close}
        loadingLabel={copy.loading}
        unavailableLabel={copy.loadError}
		footerActions={!isWireGuard && !isHysteria2 ? (
          <ShareAccessActions
			vlessLink={connection?.vlessLink ?? ''}
            profileName={connection?.profile.name}
            includeQrShare
            compact
          />
		) : undefined}
      />
    </div>
  );
}
