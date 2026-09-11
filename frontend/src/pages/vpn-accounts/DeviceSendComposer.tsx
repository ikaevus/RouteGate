import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import {
  createVpnAccountDelivery,
  getDeliveryProviders,
  getTelegramPairing,
  getTelegramRecipients,
  getVpnAccountDeliveries,
  previewVpnAccountDelivery,
  startTelegramPairingForAccount,
  type CreateDeliveryRequest,
  type DeliveryChannel,
  type DeliveryLocale,
  type DeliveryTemplate,
  type TelegramPairingSession,
} from '../../entities/delivery/api/deliveryApi';
import { getVpnAccount } from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { ApiError } from '../../shared/api/client';
import { getCurrentLocale, t } from '../../shared/i18n/i18n';
import { ScannableQrCode } from '../../shared/qr/ScannableQrCode';
import '../settings/TelegramRecipientsPanel.css';
import './vpn-access-delivery.css';

type DeviceSendComposerProps = {
  accountId: string;
  deviceId: string;
  deviceName: string;
  accessUrl: string;
  onClose: () => void;
};

type SendVariables = { request: CreateDeliveryRequest; idempotencyKey: string };

function channelLabel(channel: DeliveryChannel): string {
  return channel === 'telegram' ? t('delivery.telegram') : t('delivery.email');
}

function configureChannelLabel(channel: DeliveryChannel): string {
  return channel === 'telegram' ? t('delivery.configureTelegramAction') : t('delivery.configureEmailAction');
}

function formatPairingTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    switch (error.code) {
      case 'device_access_url_stale': return t('delivery.deviceAccessUrlStale');
      case 'device_revoked': return t('delivery.deviceRevoked');
      case 'device_not_found': return t('delivery.deviceRevoked');
      case 'smtp_not_configured':
      case 'smtp_configuration_invalid': return t('delivery.configureSmtp');
      case 'telegram_not_configured':
      case 'telegram_configuration_invalid':
      case 'telegram_unauthorized': return t('delivery.configureTelegram');
      case 'telegram_invalid_chat_id':
      case 'telegram_forbidden':
      case 'telegram_bad_request':
      case 'telegram_not_found': return t('delivery.telegramRelationship');
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

// The device card owns Send: this composer is embedded inline in an Access &
// Devices device card (opened from that device's Send button), scoped to
// exactly that device's currently-revealed access URL. It reuses the same
// backend delivery queue/history/providers/Telegram-pairing as the rest of
// RouteGate delivery - there is deliberately no separate device-level
// delivery stack.
export function DeviceSendComposer({ accountId, deviceId, deviceName, accessUrl, onClose }: DeviceSendComposerProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [channel, setChannel] = useState<DeliveryChannel>('email');
  const [recipient, setRecipient] = useState('');
  const [recipientSeeded, setRecipientSeeded] = useState(false);
  const [locale, setLocale] = useState<DeliveryLocale>(getCurrentLocale());
  const [template, setTemplate] = useState<DeliveryTemplate>('vpn_access');
  const [attachQr, setAttachQr] = useState(false);
  const [idempotencyKey, setIdempotencyKey] = useState<string | null>(null);
  const [queuedNotice, setQueuedNotice] = useState(false);
  const [accountPairing, setAccountPairing] = useState<TelegramPairingSession | null>(null);

  const accountQuery = useQuery({
    queryKey: ['vpn-account', accountId],
    queryFn: () => getVpnAccount(accountId),
  });
  const providersQuery = useQuery({ queryKey: ['delivery-providers'], queryFn: getDeliveryProviders });
  const historyQuery = useQuery({
    queryKey: ['vpn-account-deliveries', accountId],
    queryFn: () => getVpnAccountDeliveries(accountId),
  });

  const selectedProvider = useMemo(
    () => providersQuery.data?.items.find((item) => item.channel === channel),
    [channel, providersQuery.data],
  );

  const telegramRecipientsQuery = useQuery({
    queryKey: ['delivery-telegram-recipients'],
    queryFn: getTelegramRecipients,
    enabled: channel === 'telegram' && selectedProvider?.ready === true,
  });

  const accountPairingQuery = useQuery({
    queryKey: ['vpn-account-telegram-pairing', accountPairing?.id],
    queryFn: () => getTelegramPairing(accountPairing?.id ?? ''),
    enabled: Boolean(accountPairing?.id && accountPairing.state === 'pending'),
    refetchInterval: (query) => query.state.data?.state === 'pending' ? 2000 : false,
    retry: false,
  });

  const previewQuery = useQuery({
    queryKey: ['vpn-account-delivery-preview', accountId, locale, template],
    queryFn: () => previewVpnAccountDelivery(accountId, { locale, template }),
    enabled: selectedProvider?.ready === true,
    retry: false,
  });

  useEffect(() => {
    if (!recipientSeeded && channel === 'email' && accountQuery.data) {
      setRecipient(accountQuery.data.email?.trim() ?? '');
      setRecipientSeeded(true);
    }
  }, [accountQuery.data, channel, recipientSeeded]);

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

  useEffect(() => {
    const current = accountPairingQuery.data;
    if (!current || !accountPairing) return;
    const merged = { ...current, deepLink: accountPairing.deepLink };
    setAccountPairing(merged);
    if (current.state === 'paired') {
      void queryClient.invalidateQueries({ queryKey: ['delivery-telegram-recipients'] });
      if (current.recipient) {
        setRecipient(current.recipient.recipient);
      }
    }
  }, [accountPairingQuery.data, accountPairing, queryClient]);

  const startAccountPairingMutation = useMutation({
    mutationFn: () => startTelegramPairingForAccount(accountId),
    onSuccess: (session) => setAccountPairing(session),
  });

  const sendMutation = useMutation({
    mutationFn: ({ request, idempotencyKey: requestKey }: SendVariables) => createVpnAccountDelivery(accountId, request, requestKey),
    onSuccess: async () => {
      setQueuedNotice(true);
      setIdempotencyKey(null);
      await queryClient.invalidateQueries({ queryKey: ['vpn-account-deliveries', accountId] });
    },
  });

  function selectChannel(nextChannel: DeliveryChannel) {
    setQueuedNotice(false);
    setChannel(nextChannel);
    setIdempotencyKey(null);
    setAccountPairing(null);
    if (nextChannel === 'email') {
      setRecipient(accountQuery.data?.email?.trim() ?? '');
      setRecipientSeeded(true);
    } else {
      setRecipient('');
    }
  }

  function updateRecipient(value: string) { setRecipient(value); setIdempotencyKey(null); }
  function updateLocale(value: DeliveryLocale) { setLocale(value); setIdempotencyKey(null); }
  function updateTemplate(value: DeliveryTemplate) { setTemplate(value); setIdempotencyKey(null); }
  function updateAttachQr(value: boolean) { setAttachQr(value); setIdempotencyKey(null); }

  function openProviderSettings() {
    navigate(`/settings?focus=delivery&channel=${channel}`);
  }

  function openTelegramRecipients() {
    navigate('/settings?focus=delivery&channel=telegram#telegram-recipients');
  }

  function queueDelivery() {
    const normalizedRecipient = recipient.trim();
    if (!selectedProvider?.ready || normalizedRecipient === '' || previewQuery.isError || !previewQuery.data) return;
    const requestKey = idempotencyKey ?? newIdempotencyKey();
    if (!idempotencyKey) setIdempotencyKey(requestKey);
    sendMutation.mutate({
      idempotencyKey: requestKey,
      request: { channel, recipient: normalizedRecipient, locale, template, attachQr, deviceId, accessUrl },
    });
  }

  const deviceHistory = (historyQuery.data?.items ?? []).filter((item) => item.deviceId === deviceId);
  const telegramRecipients = telegramRecipientsQuery.data?.items ?? [];
  const canSend = Boolean(selectedProvider?.ready && recipient.trim() !== '' && previewQuery.data && !previewQuery.isError && !sendMutation.isPending);

  return (
    <div className="feature-subpanel vpn-access-delivery-composer vpn-device-send-composer">
      <div className="vpn-access-delivery-channel-actions">
        <button className={`small-button${channel === 'email' ? ' vpn-device-send-channel-active' : ''}`} type="button" onClick={() => selectChannel('email')} disabled={providersQuery.isLoading}>
          {t('delivery.sendViaEmail')}
        </button>
        <button className={`small-button${channel === 'telegram' ? ' vpn-device-send-channel-active' : ''}`} type="button" onClick={() => selectChannel('telegram')} disabled={providersQuery.isLoading}>
          {t('delivery.sendViaTelegram')}
        </button>
        <button className="small-button" type="button" onClick={onClose}>{t('delivery.cancel')}</button>
      </div>

      {providersQuery.isLoading && <p className="empty-state">{t('delivery.providerLoading')}</p>}
      {providersQuery.isError && <div className="form-message form-message-error">{t('delivery.providerLoadError')}</div>}
      {queuedNotice && <div className="form-message form-message-success">{t('delivery.queuedSuccess')}</div>}

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
                <span>{t('delivery.recipient')}</span>
                <input
                  type="email"
                  value={recipient}
                  placeholder={t('delivery.recipientPlaceholder')}
                  onChange={(event) => updateRecipient(event.target.value)}
                />
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
            <div className="form-message form-message-warning">{t('telegramPairing.recipientRequired')}</div>
          )}

          {channel === 'telegram' && (
            <div className="vpn-access-delivery-telegram-link">
              <button
                className="small-button"
                type="button"
                disabled={startAccountPairingMutation.isPending || accountPairing?.state === 'pending'}
                onClick={() => startAccountPairingMutation.mutate()}
              >
                {startAccountPairingMutation.isPending ? t('telegramPairing.connecting') : t('telegramPairing.linkNewRecipient')}
              </button>
              <button className="small-button" type="button" onClick={openTelegramRecipients}>
                {t('telegramPairing.manageRecipients')}
              </button>
            </div>
          )}

          {startAccountPairingMutation.isError && (
            <div className="form-message form-message-error">{t('telegramPairing.error')}</div>
          )}

          {accountPairing && accountPairing.state === 'pending' && (
            <div className="telegram-pairing-session">
              <div className="telegram-pairing-copy">
                <strong>{t('telegramPairing.instructionsTitle')}</strong>
                <p>{t('telegramPairing.instructions')}</p>
                <p>{t('telegramPairing.linkedToAccount')}</p>
                {accountPairing.deepLink && (
                  <a className="primary-button telegram-pairing-link" href={accountPairing.deepLink} target="_blank" rel="noreferrer">
                    {t('telegramPairing.openTelegram')}
                  </a>
                )}
                <p className="telegram-pairing-waiting">{t('telegramPairing.waiting')}</p>
                <small>{t('telegramPairing.expires', { time: formatPairingTime(accountPairing.expiresAt) })}</small>
                {accountPairing.errorCode === 'telegram_pairing_webhook_conflict' && (
                  <div className="form-message form-message-error">{t('telegramPairing.webhookConflict')}</div>
                )}
              </div>
              {accountPairing.deepLink && <ScannableQrCode value={accountPairing.deepLink} showHeader={false} />}
            </div>
          )}

          {accountPairing?.state === 'expired' && <div className="form-message form-message-warning">{t('telegramPairing.expired')}</div>}
          {accountPairing?.state === 'paired' && <div className="form-message form-message-success">{t('telegramPairing.paired')}</div>}

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
            {previewQuery.data && (
              <div className="vpn-access-delivery-preview-body">
                <strong>{previewQuery.data.subject}</strong>
                <pre>{previewQuery.data.text}</pre>
              </div>
            )}
          </div>

          {sendMutation.isError && <div className="form-message form-message-error">{errorMessage(sendMutation.error, t('delivery.sendError'))}</div>}
          <div className="form-actions">
            <button className="primary-button" type="button" disabled={!canSend} onClick={queueDelivery}>
              {sendMutation.isPending ? t('delivery.sendingRequest') : t('delivery.sendForDevice', { device: deviceName })}
            </button>
          </div>
        </>
      )}

      {deviceHistory.length > 0 && (
        <details className="vpn-device-send-history">
          <summary>{t('delivery.historyTitle')}</summary>
          {deviceHistory.map((item) => (
            <div className="vpn-access-delivery-history-item" key={item.id}>
              <div className="vpn-access-delivery-history-main">
                <span>{item.recipientDisplay}</span>
                <span>{item.status}</span>
              </div>
            </div>
          ))}
        </details>
      )}
    </div>
  );
}
