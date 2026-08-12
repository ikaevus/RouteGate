import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  createVpnAccountDelivery,
  getDeliveryProviders,
  getTelegramRecipients,
  getVpnAccountDeliveries,
  previewVpnAccountDelivery,
  retryDelivery,
  type CreateDeliveryRequest,
  type DeliveryChannel,
  type DeliveryLocale,
  type DeliveryRecord,
  type DeliveryStatus,
  type DeliveryTemplate,
} from '../../entities/delivery/api/deliveryApi';
import { getVpnAccount } from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { ApiError } from '../../shared/api/client';
import { getCurrentLocale, t } from '../../shared/i18n/i18n';
import './vpn-access-delivery.css';

type VpnAccessDeliveryPanelProps = { accountId: string };
type SendVariables = { request: CreateDeliveryRequest; idempotencyKey: string };

const activeStatuses: DeliveryStatus[] = ['queued', 'sending', 'retrying'];

function formatDate(value?: string | null): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function deliveryStatusLabel(status: DeliveryStatus): string {
  switch (status) {
    case 'queued': return t('delivery.statusQueued');
    case 'sending': return t('delivery.statusSending');
    case 'retrying': return t('delivery.statusRetrying');
    case 'sent': return t('delivery.statusSent');
    case 'delivered': return t('delivery.statusDelivered');
    case 'failed': return t('delivery.statusFailed');
    case 'uncertain': return t('delivery.statusUncertain');
    default: return status;
  }
}

function channelLabel(channel: DeliveryChannel): string {
  switch (channel) {
    case 'telegram': return t('delivery.telegram');
    case 'whatsapp': return t('delivery.whatsapp');
    default: return t('delivery.email');
  }
}

function configureChannelLabel(channel: DeliveryChannel): string {
  switch (channel) {
    case 'telegram': return t('delivery.configureTelegramAction');
    case 'whatsapp': return t('delivery.configureWhatsAppAction');
    default: return t('delivery.configureEmailAction');
  }
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    switch (error.code) {
      case 'vpn_server_missing':
      case 'vpn_endpoint_missing':
      case 'vpn_reality_incomplete':
      case 'vpn_access_incomplete':
      case 'vpn_access_unavailable': return t('delivery.fixVpnAccess');
      case 'smtp_not_configured':
      case 'smtp_configuration_invalid': return t('delivery.configureSmtp');
      case 'telegram_not_configured':
      case 'telegram_configuration_invalid':
      case 'telegram_unauthorized': return t('delivery.configureTelegram');
      case 'telegram_invalid_chat_id':
      case 'telegram_forbidden':
      case 'telegram_bad_request':
      case 'telegram_not_found': return t('delivery.telegramRelationship');
      case 'whatsapp_not_configured':
      case 'whatsapp_configuration_invalid':
      case 'whatsapp_unauthorized': return t('delivery.configureWhatsApp');
      case 'whatsapp_invalid_recipient':
      case 'whatsapp_bad_request':
      case 'whatsapp_forbidden':
      case 'whatsapp_not_found':
      case 'whatsapp_template_unsupported': return t('delivery.whatsappFailure');
      case 'public_url_missing':
      case 'public_url_invalid': return t('delivery.configurePublicUrl');
      default: return fallback;
    }
  }
  return error instanceof Error && error.message.trim() !== '' ? error.message : fallback;
}

function newIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return `ui-${crypto.randomUUID()}`;
  }
  return `ui-${Date.now()}-${Math.random().toString(36).slice(2, 14)}`;
}

function recipientLabel(channel: DeliveryChannel): string {
  if (channel === 'whatsapp') return t('delivery.whatsappPhone');
  return t('delivery.recipient');
}

function recipientPlaceholder(channel: DeliveryChannel): string {
  if (channel === 'whatsapp') return t('delivery.whatsappPhonePlaceholder');
  return t('delivery.recipientPlaceholder');
}

export function VpnAccessDeliveryPanel({ accountId }: VpnAccessDeliveryPanelProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const [isComposerOpen, setIsComposerOpen] = useState(searchParams.get('sendAccess') === '1');
  const [channel, setChannel] = useState<DeliveryChannel>('email');
  const [recipient, setRecipient] = useState('');
  const [recipientSeededFor, setRecipientSeededFor] = useState('');
  const [locale, setLocale] = useState<DeliveryLocale>(getCurrentLocale());
  const [template, setTemplate] = useState<DeliveryTemplate>('vpn_access');
  const [attachQr, setAttachQr] = useState(false);
  const [idempotencyKey, setIdempotencyKey] = useState<string | null>(null);
  const [queuedNotice, setQueuedNotice] = useState(false);

  const accountQuery = useQuery({
    queryKey: ['vpn-account', accountId],
    queryFn: () => getVpnAccount(accountId),
    enabled: accountId !== '',
  });
  const providersQuery = useQuery({ queryKey: ['delivery-providers'], queryFn: getDeliveryProviders });
  const historyQuery = useQuery({
    queryKey: ['vpn-account-deliveries', accountId],
    queryFn: () => getVpnAccountDeliveries(accountId),
    enabled: accountId !== '',
    refetchInterval: (query) => query.state.data?.items.some((item) => activeStatuses.includes(item.status)) ? 2000 : false,
  });

  const selectedProvider = useMemo(
    () => providersQuery.data?.items.find((item) => item.channel === channel),
    [channel, providersQuery.data],
  );

  const telegramRecipientsQuery = useQuery({
    queryKey: ['delivery-telegram-recipients'],
    queryFn: getTelegramRecipients,
    enabled: isComposerOpen && channel === 'telegram' && selectedProvider?.ready === true,
  });

  const previewQuery = useQuery({
    queryKey: ['vpn-account-delivery-preview', accountId, locale, template],
    queryFn: () => previewVpnAccountDelivery(accountId, { locale, template }),
    enabled: isComposerOpen && accountId !== '' && selectedProvider?.ready === true,
    retry: false,
  });

  useEffect(() => {
    setChannel('email');
    setRecipient('');
    setRecipientSeededFor('');
    setLocale(getCurrentLocale());
    setTemplate('vpn_access');
    setAttachQr(false);
    setIdempotencyKey(null);
    setQueuedNotice(false);
  }, [accountId]);

  useEffect(() => {
    if (searchParams.get('sendAccess') === '1') setIsComposerOpen(true);
  }, [searchParams]);

  useEffect(() => {
    if (channel === 'email' && recipientSeededFor !== accountId && accountQuery.data) {
      setRecipient(accountQuery.data.email?.trim() ?? '');
      setRecipientSeededFor(accountId);
    }
  }, [accountId, accountQuery.data, channel, recipientSeededFor]);

  useEffect(() => {
    if (channel !== 'telegram') return;
    const items = telegramRecipientsQuery.data?.items ?? [];
    if (recipient !== '' && !items.some((item) => item.recipient === recipient)) {
      setRecipient('');
      return;
    }
    if (recipient === '' && items.length === 1) {
      setRecipient(items[0].recipient);
    }
  }, [channel, recipient, telegramRecipientsQuery.data]);

  const sendMutation = useMutation({
    mutationFn: ({ request, idempotencyKey: requestKey }: SendVariables) => createVpnAccountDelivery(accountId, request, requestKey),
    onSuccess: async () => {
      setQueuedNotice(true);
      setIdempotencyKey(null);
      setIsComposerOpen(false);
      clearSendAccessParam();
      await queryClient.invalidateQueries({ queryKey: ['vpn-account-deliveries', accountId] });
    },
  });
  const retryMutation = useMutation({
    mutationFn: (deliveryId: string) => retryDelivery(deliveryId),
    onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['vpn-account-deliveries', accountId] }),
  });

  function clearSendAccessParam() {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      next.delete('sendAccess');
      return next;
    }, { replace: true });
  }

  function openComposer() {
    setQueuedNotice(false);
    setIsComposerOpen(true);
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      next.set('sendAccess', '1');
      return next;
    }, { replace: true });
  }

  function closeComposer() {
    setIsComposerOpen(false);
    setIdempotencyKey(null);
    clearSendAccessParam();
  }

  function openProviderSettings() {
    navigate(`/settings?focus=delivery&channel=${channel}`);
  }

  function openTelegramRecipients() {
    navigate('/settings?focus=delivery&channel=telegram#telegram-recipients');
  }

  function updateChannel(value: DeliveryChannel) {
    setChannel(value);
    setAttachQr(false);
    setIdempotencyKey(null);
    if (value !== 'email') {
      setRecipient('');
      return;
    }
    setRecipient(accountQuery.data?.email?.trim() ?? '');
    setRecipientSeededFor(accountId);
  }

  function updateRecipient(value: string) { setRecipient(value); setIdempotencyKey(null); }
  function updateLocale(value: DeliveryLocale) { setLocale(value); setIdempotencyKey(null); }
  function updateTemplate(value: DeliveryTemplate) { setTemplate(value); setIdempotencyKey(null); }
  function updateAttachQr(value: boolean) { setAttachQr(value); setIdempotencyKey(null); }

  function queueDelivery() {
    const normalizedRecipient = recipient.trim();
    if (!selectedProvider?.ready || normalizedRecipient === '' || previewQuery.isError || !previewQuery.data) return;
    const requestKey = idempotencyKey ?? newIdempotencyKey();
    if (!idempotencyKey) setIdempotencyKey(requestKey);
    sendMutation.mutate({
      idempotencyKey: requestKey,
      request: { channel, recipient: normalizedRecipient, locale, template, attachQr },
    });
  }

  const history = historyQuery.data?.items ?? [];
  const telegramRecipients = telegramRecipientsQuery.data?.items ?? [];
  const canSend = Boolean(selectedProvider?.ready && recipient.trim() !== '' && previewQuery.data && !previewQuery.isError && !sendMutation.isPending);

  return (
    <div className="panel feature-detail-panel vpn-access-delivery-panel">
      <div className="panel-header">
        <div>
          <div className="panel-title">{t('delivery.title')}</div>
          <p className="panel-subtitle">{t('delivery.subtitle')}</p>
        </div>
        {!isComposerOpen && (
          <button className="primary-button" type="button" onClick={openComposer} disabled={providersQuery.isLoading}>
            {t('delivery.openComposer')}
          </button>
        )}
      </div>

      {providersQuery.isLoading && !isComposerOpen && <p className="empty-state">{t('delivery.providerLoading')}</p>}
      {providersQuery.isError && <div className="form-message form-message-error">{t('delivery.providerLoadError')}</div>}
      {queuedNotice && <div className="form-message form-message-success">{t('delivery.queuedSuccess')}</div>}

      {isComposerOpen && (
        <div className="feature-subpanel vpn-access-delivery-composer">
          <label className="field vpn-access-delivery-channel-field">
            <span>{t('delivery.channel')}</span>
            <select value={channel} onChange={(event) => updateChannel(event.target.value as DeliveryChannel)}>
              <option value="email">{t('delivery.email')}</option>
              <option value="telegram">{t('delivery.telegram')}</option>
              <option value="whatsapp">{t('delivery.whatsapp')}</option>
            </select>
          </label>

          {providersQuery.isLoading && <p className="empty-state">{t('delivery.providerLoading')}</p>}

          {!providersQuery.isLoading && !providersQuery.isError && selectedProvider?.ready !== true && (
            <div className="vpn-access-delivery-next-action">
              <div>
                <strong>{t('delivery.providerSetupTitle', { channel: channelLabel(channel) })}</strong>
                <p>{t('delivery.providerSetupDescription')}</p>
              </div>
              <div className="form-actions">
                <button className="primary-button" type="button" onClick={openProviderSettings}>
                  {configureChannelLabel(channel)}
                </button>
                <button className="small-button" type="button" onClick={closeComposer}>{t('delivery.cancel')}</button>
              </div>
            </div>
          )}

          {!providersQuery.isLoading && !providersQuery.isError && selectedProvider?.ready === true && (
            <>
              <div className="vpn-access-delivery-fields">
                {channel === 'telegram' ? (
                  <label className="field">
                    <span>{t('telegramPairing.selectRecipient')}</span>
                    <select value={recipient} onChange={(event) => updateRecipient(event.target.value)} disabled={telegramRecipientsQuery.isLoading}>
                      <option value="">{t('telegramPairing.selectPlaceholder')}</option>
                      {telegramRecipients.map((item) => (
                        <option value={item.recipient} key={item.id}>
                          {item.displayName}{item.username ? ` (@${item.username})` : ''}
                        </option>
                      ))}
                    </select>
                    {telegramRecipientsQuery.isError && <small>{t('telegramPairing.loadError')}</small>}
                  </label>
                ) : (
                  <label className="field">
                    <span>{recipientLabel(channel)}</span>
                    <input
                      type={channel === 'email' ? 'email' : 'tel'}
                      inputMode={channel === 'whatsapp' ? 'tel' : undefined}
                      value={recipient}
                      placeholder={recipientPlaceholder(channel)}
                      onChange={(event) => updateRecipient(event.target.value)}
                    />
                    {channel === 'whatsapp' && <small>{t('delivery.whatsappPrerequisite')}</small>}
                  </label>
                )}
                <label className="field">
                  <span>{t('delivery.language')}</span>
                  <select value={locale} onChange={(event) => updateLocale(event.target.value as DeliveryLocale)}>
                    <option value="en">{t('delivery.languageEnglish')}</option>
                    <option value="ru">{t('delivery.languageRussian')}</option>
                  </select>
                </label>
                <label className="field">
                  <span>{t('delivery.template')}</span>
                  <select value={template} onChange={(event) => updateTemplate(event.target.value as DeliveryTemplate)}>
                    <option value="vpn_access">{t('delivery.templateAccess')}</option>
                    <option value="vpn_access_reissued">{t('delivery.templateReissued')}</option>
                  </select>
                </label>
              </div>

              {channel === 'telegram' && !telegramRecipientsQuery.isLoading && !telegramRecipientsQuery.isError && telegramRecipients.length === 0 && (
                <div className="vpn-access-delivery-next-action">
                  <div>
                    <strong>{t('telegramPairing.recipientRequired')}</strong>
                  </div>
                  <button className="primary-button" type="button" onClick={openTelegramRecipients}>
                    {t('telegramPairing.manageRecipients')}
                  </button>
                </div>
              )}

              {selectedProvider.capabilities.Attachments && (
                <label className="vpn-access-delivery-checkbox">
                  <input type="checkbox" checked={attachQr} onChange={(event) => updateAttachQr(event.target.checked)} />
                  <span>{t('delivery.attachQr')}</span>
                </label>
              )}

              <div className="vpn-access-delivery-preview">
                <strong>{t('delivery.preview')}</strong>
                {previewQuery.isLoading && <p>{t('delivery.previewLoading')}</p>}
                {previewQuery.isError && <div className="form-message form-message-warning">{errorMessage(previewQuery.error, t('delivery.previewUnavailable'))}</div>}
                {previewQuery.data && channel === 'whatsapp' && (
                  <div className="vpn-access-delivery-preview-body">
                    <p>{t('delivery.whatsappPreview')}</p>
                  </div>
                )}
                {previewQuery.data && channel !== 'whatsapp' && (
                  <div className="vpn-access-delivery-preview-body">
                    <strong>{previewQuery.data.subject}</strong>
                    <pre>{previewQuery.data.text}</pre>
                  </div>
                )}
              </div>

              {sendMutation.isError && <div className="form-message form-message-error">{errorMessage(sendMutation.error, t('delivery.sendError'))}</div>}
              <div className="form-actions">
                <button className="primary-button" type="button" disabled={!canSend} onClick={queueDelivery}>
                  {sendMutation.isPending ? t('delivery.sendingRequest') : t('delivery.send')}
                </button>
                <button className="small-button" type="button" onClick={closeComposer}>{t('delivery.cancel')}</button>
              </div>
            </>
          )}
        </div>
      )}

      <div className="vpn-access-delivery-history">
        <div className="panel-title">{t('delivery.historyTitle')}</div>
        {historyQuery.isLoading && <p className="empty-state">{t('delivery.historyLoading')}</p>}
        {historyQuery.isError && <div className="form-message form-message-error">{t('delivery.historyError')}</div>}
        {!historyQuery.isLoading && !historyQuery.isError && history.length === 0 && <p className="empty-state">{t('delivery.historyEmpty')}</p>}
        {history.map((item) => (
          <DeliveryHistoryItem
            item={item}
            key={item.id}
            retryPending={retryMutation.isPending}
            onRetry={() => retryMutation.mutate(item.id)}
          />
        ))}
        {retryMutation.isError && <div className="form-message form-message-error">{t('delivery.retryError')}</div>}
      </div>
    </div>
  );
}

function DeliveryHistoryItem({ item, retryPending, onRetry }: { item: DeliveryRecord; retryPending: boolean; onRetry: () => void }) {
  const canRetry = item.status === 'failed' || item.status === 'uncertain';
  const telegramRelationshipFailure = item.channel === 'telegram'
    && ['telegram_forbidden', 'telegram_bad_request', 'telegram_not_found'].includes(item.lastErrorCode ?? '');
  const whatsAppFailure = item.channel === 'whatsapp'
    && ['whatsapp_bad_request', 'whatsapp_forbidden', 'whatsapp_not_found', 'whatsapp_template_unsupported'].includes(item.lastErrorCode ?? '');
  return (
    <div className="vpn-access-delivery-history-item">
      <div className="vpn-access-delivery-history-main">
        <strong>{deliveryStatusLabel(item.status)}</strong>
        <span>{t('delivery.recipientDisplay')}: {item.recipientDisplay}</span>
        <span>{t('delivery.created')}: {formatDate(item.createdAt)}</span>
        {item.sentAt && <span>{t('delivery.sentAt')}: {formatDate(item.sentAt)}</span>}
        <span>{t('delivery.attempt')}: {item.attemptCount}/{item.maxAttempts}</span>
      </div>
      {item.status === 'uncertain' && <p className="vpn-access-delivery-history-hint">{t('delivery.uncertainHint')}</p>}
      {item.status === 'failed' && (
        <p className="vpn-access-delivery-history-hint">
          {telegramRelationshipFailure
            ? t('delivery.telegramRelationship')
            : whatsAppFailure
              ? t('delivery.whatsappFailure')
              : t('delivery.failedHint')}
        </p>
      )}
      {canRetry && (
        <button className="small-button" type="button" disabled={retryPending} onClick={onRetry}>
          {retryPending ? t('delivery.retryingAction') : t('delivery.retry')}
        </button>
      )}
    </div>
  );
}