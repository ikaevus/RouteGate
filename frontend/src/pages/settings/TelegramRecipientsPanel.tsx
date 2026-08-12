import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  getDeliveryProviderSettings,
  getTelegramPairing,
  getTelegramRecipients,
  removeTelegramRecipient,
  startTelegramPairing,
  testTelegramRecipient,
  type DeliveryRecipient,
  type TelegramPairingSession,
} from '../../entities/delivery/api/deliveryApi';
import { ScannableQrCode } from '../../shared/qr/ScannableQrCode';
import { t } from '../../shared/i18n/i18n';
import './TelegramRecipientsPanel.css';

function formatTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export function TelegramRecipientsPanel() {
  const queryClient = useQueryClient();
  const [pairing, setPairing] = useState<TelegramPairingSession | null>(null);
  const [testSuccessId, setTestSuccessId] = useState('');

  const settingsQuery = useQuery({
    queryKey: ['delivery-provider-settings', 'telegram'],
    queryFn: () => getDeliveryProviderSettings('telegram'),
  });
  const ready = settingsQuery.data?.ready === true;
  const recipientsQuery = useQuery({
    queryKey: ['delivery-telegram-recipients'],
    queryFn: getTelegramRecipients,
    enabled: ready,
  });
  const pairingQuery = useQuery({
    queryKey: ['delivery-telegram-pairing', pairing?.id],
    queryFn: () => getTelegramPairing(pairing?.id ?? ''),
    enabled: Boolean(pairing?.id && pairing.state === 'pending'),
    refetchInterval: (query) => query.state.data?.state === 'pending' ? 2000 : false,
    retry: false,
  });

  useEffect(() => {
    const current = pairingQuery.data;
    if (!current || !pairing) return;
    const merged = { ...current, deepLink: pairing.deepLink };
    setPairing(merged);
    if (current.state === 'paired') {
      void queryClient.invalidateQueries({ queryKey: ['delivery-telegram-recipients'] });
    }
  }, [pairingQuery.data, queryClient]);

  const startMutation = useMutation({
    mutationFn: startTelegramPairing,
    onSuccess: (session) => {
      setPairing(session);
      setTestSuccessId('');
    },
  });

  const testMutation = useMutation({
    mutationFn: (recipientId: string) => testTelegramRecipient(recipientId),
    onSuccess: (result, recipientId) => setTestSuccessId(result.ok ? recipientId : ''),
  });

  const removeMutation = useMutation({
    mutationFn: (recipientId: string) => removeTelegramRecipient(recipientId),
    onSuccess: async () => {
      setTestSuccessId('');
      await queryClient.invalidateQueries({ queryKey: ['delivery-telegram-recipients'] });
    },
  });

  function removeRecipient(recipient: DeliveryRecipient) {
    if (!window.confirm(t('telegramPairing.removeConfirm', { name: recipient.displayName }))) return;
    removeMutation.mutate(recipient.id);
  }

  const recipients = recipientsQuery.data?.items ?? [];
  const pairingError = pairing?.errorCode;

  return (
    <section className="panel settings-panel telegram-recipients-panel" id="telegram-recipients">
      <div className="settings-panel-heading">
        <div>
          <div className="panel-title">{t('telegramPairing.title')}</div>
          <p className="panel-subtitle">{t('telegramPairing.subtitle')}</p>
        </div>
        {ready && (
          <button className="primary-button" type="button" disabled={startMutation.isPending} onClick={() => startMutation.mutate()}>
            {startMutation.isPending ? t('telegramPairing.connecting') : t('telegramPairing.connect')}
          </button>
        )}
      </div>

      {!ready && !settingsQuery.isLoading && <div className="form-message form-message-warning">{t('telegramPairing.configureFirst')}</div>}
      {startMutation.isError && <div className="form-message form-message-error">{t('telegramPairing.error')}</div>}

      {pairing && pairing.state === 'pending' && (
        <div className="telegram-pairing-session">
          <div className="telegram-pairing-copy">
            <strong>{t('telegramPairing.instructionsTitle')}</strong>
            <p>{t('telegramPairing.instructions')}</p>
            {pairing.deepLink && (
              <a className="primary-button telegram-pairing-link" href={pairing.deepLink} target="_blank" rel="noreferrer">
                {t('telegramPairing.openTelegram')}
              </a>
            )}
            <p className="telegram-pairing-waiting">{t('telegramPairing.waiting')}</p>
            <small>{t('telegramPairing.expires', { time: formatTime(pairing.expiresAt) })}</small>
            {pairingError === 'telegram_pairing_webhook_conflict' && (
              <div className="form-message form-message-error">{t('telegramPairing.webhookConflict')}</div>
            )}
          </div>
          {pairing.deepLink && <ScannableQrCode value={pairing.deepLink} showHeader={false} />}
        </div>
      )}

      {pairing?.state === 'expired' && <div className="form-message form-message-warning">{t('telegramPairing.expired')}</div>}
      {pairing?.state === 'paired' && <div className="form-message form-message-success">{t('telegramPairing.paired')}</div>}

      {recipientsQuery.isError && <div className="form-message form-message-error">{t('telegramPairing.loadError')}</div>}
      {ready && !recipientsQuery.isLoading && !recipientsQuery.isError && recipients.length === 0 && (
        <p className="empty-state">{t('telegramPairing.listEmpty')}</p>
      )}

      {recipients.length > 0 && (
        <div className="telegram-recipient-list">
          {recipients.map((recipient) => {
            const testPending = testMutation.isPending && testMutation.variables === recipient.id;
            const removePending = removeMutation.isPending && removeMutation.variables === recipient.id;
            const testFailed = testMutation.isSuccess && testMutation.variables === recipient.id && !testMutation.data.ok;
            return (
              <div className="telegram-recipient-row" key={recipient.id}>
                <div>
                  <strong>{recipient.displayName}</strong>
                  {recipient.username && <small>{t('telegramPairing.username', { username: recipient.username })}</small>}
                </div>
                <div className="telegram-recipient-actions">
                  <button className="small-button" type="button" disabled={testPending} onClick={() => testMutation.mutate(recipient.id)}>
                    {testPending ? t('telegramPairing.testing') : t('telegramPairing.test')}
                  </button>
                  <button className="small-button" type="button" disabled={removePending} onClick={() => removeRecipient(recipient)}>
                    {removePending ? t('telegramPairing.removing') : t('telegramPairing.remove')}
                  </button>
                </div>
                {testSuccessId === recipient.id && <div className="form-message form-message-success">{t('telegramPairing.testSuccess')}</div>}
                {testFailed && <div className="form-message form-message-error">{t('telegramPairing.testFailed')}</div>}
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}
