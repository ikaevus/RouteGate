import { useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import {
  createVpnAccountSubscriptionToken,
  getPublicSubscription,
  getVpnAccountSubscriptionQRCode,
  rotateVpnAccountSubscriptionToken,
  type SubscriptionTokenResponse,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { getCurrentLocale, t } from '../../shared/i18n/i18n';
import { StatusBadge } from '../../shared/ui/StatusBadge';
import { SubscriptionQrDialog } from '../../shared/ui/SubscriptionQrDialog';
import './vpn-client-connection.css';

type VpnClientConnectionPanelProps = {
  accountId: string;
};

function formatDate(value?: string | null): string {
  if (!value) {
    return t('common.notAvailable');
  }

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatValue(value?: string | null): string {
  return value && value.trim() !== '' ? value : t('common.notAvailable');
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="detail-row">
      <span>{label}</span>
      <strong>{children}</strong>
    </div>
  );
}

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      title: 'Подключение VPN-клиента',
      subtitle: 'Создайте QR-код или VLESS-ссылку для V2Box, V2RayTun и других клиентов с поддержкой VLESS Reality.',
      create: 'Создать подключение',
      creating: 'Создание...',
      rotate: 'Обновить URL подписки',
      rotating: 'Обновление...',
      createError: 'Не удалось подготовить подключение VPN-клиента.',
      emptyTitle: 'Подключение ещё не подготовлено',
      emptyDescription: 'Создайте подключение, чтобы получить QR-код и прямую VLESS-ссылку для VPN-клиента.',
      ready: 'Готово к подключению',
      readyDescription: 'Отсканируйте QR-код в VPN-клиенте. QR-код и кнопка копирования используют один и тот же профиль VLESS Reality.',
      format: 'VLESS Reality',
      showQr: 'Показать QR для VPN-клиента',
      copyVless: 'Скопировать VLESS-ссылку',
      copied: 'Скопировано',
      loading: 'Подготовка VLESS-профиля...',
      qrError: 'Не удалось подготовить VLESS-профиль для QR-кода.',
      credentialWarning: 'QR-код и VLESS-ссылка предоставляют доступ к VPN. Не публикуйте их и не отправляйте посторонним.',
      advanced: 'Расширенные настройки',
      advancedDescription: 'URL подписки RouteGate использует внутренний формат RouteGate и не является прямым профилем для V2Box или V2RayTun.',
      subscriptionUrl: 'URL подписки RouteGate',
      copy: 'Копировать',
      expires: 'Истекает',
      subscriptionFormat: 'Формат подписки',
      configStatus: 'Статус конфига',
      configFormat: 'Формат конфига',
      generatedAt: 'Создано',
      serverEndpoint: 'Endpoint сервера',
      configPreview: 'Превью клиентского конфига',
      configPreviewDescription: 'Технический ответ подписки и отрендеренный sing-box-конфиг для диагностики.',
      copyConfig: 'Копировать конфиг',
      loadingConfig: 'Загрузка технического превью...',
      configError: 'Не удалось загрузить техническое превью подписки.',
      noConfig: 'Отрендеренный клиентский конфиг пока недоступен.',
      qrTitle: 'QR-код для VPN-клиента',
      vlessLink: 'VLESS-ссылка',
      close: 'Закрыть',
    } as const;
  }

  return {
    title: 'Connect VPN client',
    subtitle: 'Create a QR code or VLESS link for V2Box, V2RayTun, and other VLESS Reality clients.',
    create: 'Create connection',
    creating: 'Creating...',
    rotate: 'Refresh subscription URL',
    rotating: 'Refreshing...',
    createError: 'Could not prepare the VPN client connection.',
    emptyTitle: 'Connection is not ready yet',
    emptyDescription: 'Create a connection to get a QR code and a direct VLESS link for the VPN client.',
    ready: 'Ready to connect',
    readyDescription: 'Scan the QR code in the VPN client. The QR code and copy button use the same VLESS Reality profile.',
    format: 'VLESS Reality',
    showQr: 'Show QR for VPN client',
    copyVless: 'Copy VLESS link',
    copied: 'Copied',
    loading: 'Preparing VLESS profile...',
    qrError: 'Could not prepare the VLESS profile for the QR code.',
    credentialWarning: 'The QR code and VLESS link grant VPN access. Do not publish or share them with other people.',
    advanced: 'Advanced settings',
    advancedDescription: 'The RouteGate subscription URL uses RouteGate’s internal format and is not a direct V2Box or V2RayTun profile.',
    subscriptionUrl: 'RouteGate subscription URL',
    copy: 'Copy',
    expires: 'Expires',
    subscriptionFormat: 'Subscription format',
    configStatus: 'Config status',
    configFormat: 'Config format',
    generatedAt: 'Generated at',
    serverEndpoint: 'Server endpoint',
    configPreview: 'Client config preview',
    configPreviewDescription: 'Technical subscription response and rendered sing-box config for diagnostics.',
    copyConfig: 'Copy config',
    loadingConfig: 'Loading technical preview...',
    configError: 'Could not load the technical subscription preview.',
    noConfig: 'Rendered client config is not available yet.',
    qrTitle: 'QR code for VPN client',
    vlessLink: 'VLESS link',
    close: 'Close',
  } as const;
}

export function VpnClientConnectionPanel({ accountId }: VpnClientConnectionPanelProps) {
  const copy = getCopy();
  const [subscriptionToken, setSubscriptionToken] = useState<SubscriptionTokenResponse | null>(null);
  const [copiedTarget, setCopiedTarget] = useState<string | null>(null);
  const [isQrOpen, setIsQrOpen] = useState(false);

  const qrQuery = useQuery({
    queryKey: ['vpn-account-client-connection', accountId, subscriptionToken?.subscriptionToken],
    queryFn: () => getVpnAccountSubscriptionQRCode(accountId, subscriptionToken?.subscriptionToken ?? ''),
    enabled: Boolean(subscriptionToken?.subscriptionToken),
  });

  const publicSubscriptionQuery = useQuery({
    queryKey: ['public-subscription-preview', subscriptionToken?.subscriptionToken],
    queryFn: () => getPublicSubscription(subscriptionToken?.subscriptionToken ?? ''),
    enabled: Boolean(subscriptionToken?.subscriptionToken),
  });

  const createMutation = useMutation({
    mutationFn: () => createVpnAccountSubscriptionToken(accountId),
    onMutate: () => {
      setSubscriptionToken(null);
      setCopiedTarget(null);
      setIsQrOpen(false);
    },
    onSuccess: setSubscriptionToken,
  });

  const rotateMutation = useMutation({
    mutationFn: () => rotateVpnAccountSubscriptionToken(accountId),
    onMutate: () => {
      setSubscriptionToken(null);
      setCopiedTarget(null);
      setIsQrOpen(false);
    },
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

  const isBusy = createMutation.isPending || rotateMutation.isPending;
  const qr = qrQuery.data;
  const publicSubscription = publicSubscriptionQuery.data;
  const renderedConfig = publicSubscription?.config.rendered;
  const renderedConfigText = renderedConfig
    ? JSON.stringify(renderedConfig.content, null, 2)
    : '';

  return (
    <div className="panel subscription-panel feature-detail-panel vpn-client-connection-panel">
      <div className="panel-header">
        <div>
          <div className="panel-title">{copy.title}</div>
          <p className="panel-subtitle">{copy.subtitle}</p>
        </div>

        {!subscriptionToken ? (
          <button
            className="primary-button"
            type="button"
            disabled={isBusy}
            onClick={() => createMutation.mutate()}
          >
            {createMutation.isPending ? copy.creating : copy.create}
          </button>
        ) : (
          <button
            className="small-button"
            type="button"
            disabled={isBusy}
            onClick={() => rotateMutation.mutate()}
          >
            {rotateMutation.isPending ? copy.rotating : copy.rotate}
          </button>
        )}
      </div>

      {(createMutation.isError || rotateMutation.isError) && (
        <div className="form-message form-message-error">{copy.createError}</div>
      )}

      {!subscriptionToken && !createMutation.isPending && (
        <div className="vpn-client-empty-state">
          <strong>{copy.emptyTitle}</strong>
          <p>{copy.emptyDescription}</p>
        </div>
      )}

      {subscriptionToken && (
        <div className="subscription-result subscription-self-service-card">
          <div className="form-message form-message-warning">{copy.credentialWarning}</div>

          <div className="subscription-url-stack vpn-client-primary-card">
            <div className="subscription-url-header">
              <div className="subscription-url-meta">
                <div className="subscription-url-label">{copy.ready}</div>
                <p className="subscription-url-helper">{copy.readyDescription}</p>
              </div>
              <span className="vpn-client-format-badge">{copy.format}</span>
            </div>

            {qrQuery.isLoading && <p className="empty-state">{copy.loading}</p>}
            {qrQuery.isError && (
              <div className="form-message form-message-error">{copy.qrError}</div>
            )}

            <div className="vpn-client-primary-actions">
              <button
                className="primary-button"
                type="button"
                onClick={() => setIsQrOpen(true)}
                disabled={!qr?.qrText}
              >
                {copy.showQr}
              </button>
              <button
                className="small-button"
                type="button"
                onClick={() => void copyToClipboard('vless-link', qr?.qrText ?? '')}
                disabled={!qr?.qrText}
              >
                {copiedTarget === 'vless-link' ? copy.copied : copy.copyVless}
              </button>
            </div>
          </div>

          <details className="vpn-client-advanced feature-subpanel">
            <summary>{copy.advanced}</summary>
            <div className="vpn-client-advanced-content">
              <div className="form-message form-message-warning">{copy.advancedDescription}</div>

              <div className="subscription-url-stack">
                <div className="subscription-url-header">
                  <div className="subscription-url-meta">
                    <div className="subscription-url-label">{copy.subscriptionUrl}</div>
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

              <div className="subscription-secondary-meta">
                <div className="subscription-meta-chip">
                  <span>{copy.expires}</span>
                  <strong>{formatDate(subscriptionToken.expiresAt)}</strong>
                </div>
                <div className="subscription-meta-chip">
                  <span>{copy.subscriptionFormat}</span>
                  <strong>{formatValue(publicSubscription?.format)}</strong>
                </div>
              </div>

              <div className="client-config-preview feature-subpanel">
                <div className="panel-header client-config-header">
                  <div>
                    <div className="panel-title token-snippet-title">{copy.configPreview}</div>
                    <p className="panel-subtitle">{copy.configPreviewDescription}</p>
                  </div>
                  {renderedConfig && (
                    <button
                      className="small-button"
                      type="button"
                      onClick={() => void copyToClipboard('client-config', renderedConfigText)}
                    >
                      {copiedTarget === 'client-config' ? copy.copied : copy.copyConfig}
                    </button>
                  )}
                </div>

                {publicSubscriptionQuery.isLoading && (
                  <p className="empty-state">{copy.loadingConfig}</p>
                )}
                {publicSubscriptionQuery.isError && (
                  <div className="form-message form-message-error">{copy.configError}</div>
                )}

                {publicSubscription && (
                  <div className="subscription-meta-grid">
                    <DetailRow label={copy.configStatus}>
                      <StatusBadge status={publicSubscription.config.status} />
                    </DetailRow>
                    <DetailRow label={copy.configFormat}>
                      {formatValue(renderedConfig?.format ?? publicSubscription.config.format)}
                    </DetailRow>
                    <DetailRow label={copy.generatedAt}>{formatDate(publicSubscription.generatedAt)}</DetailRow>
                    <DetailRow label={copy.serverEndpoint}>{formatValue(publicSubscription.server?.endpoint)}</DetailRow>
                  </div>
                )}

                {publicSubscription?.config.message && (
                  <div className="form-message form-message-warning">{publicSubscription.config.message}</div>
                )}

                {renderedConfig ? (
                  <pre className="code-block client-config-code">{renderedConfigText}</pre>
                ) : publicSubscription && (
                  <p className="empty-state">{copy.noConfig}</p>
                )}
              </div>
            </div>
          </details>
        </div>
      )}

      <SubscriptionQrDialog
        isOpen={isQrOpen}
        title={copy.qrTitle}
        onClose={() => setIsQrOpen(false)}
        qrText={qr?.qrText}
        qrTitle={copy.qrTitle}
        qrSubtitle={copy.format}
        url={qr?.qrText}
        urlLabel={copy.vlessLink}
        onCopyQrText={() => void copyToClipboard('qr-vless-link', qr?.qrText ?? '')}
        copyQrLabel={copy.copyVless}
        copyCopiedLabel={copy.copied}
        copied={copiedTarget === 'qr-vless-link'}
        closeLabel={copy.close}
        loadingLabel={copy.loading}
        unavailableLabel={copy.qrError}
      />
    </div>
  );
}
