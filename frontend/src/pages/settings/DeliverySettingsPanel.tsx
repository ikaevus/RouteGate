import { useEffect, useMemo, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useSearchParams } from 'react-router-dom';
import {
  getDeliveryProviders,
  type DeliveryChannel,
  type DeliveryProvider,
} from '../../entities/delivery/api/deliveryApi';
import { t } from '../../shared/i18n/i18n';
import './DeliverySettingsPanel.css';

const channels: DeliveryChannel[] = ['email', 'telegram', 'whatsapp'];

function channelLabel(channel: DeliveryChannel): string {
  switch (channel) {
    case 'telegram': return t('delivery.telegram');
    case 'whatsapp': return t('delivery.whatsapp');
    default: return t('delivery.email');
  }
}

function providerReason(code?: string): string {
  switch (code) {
    case 'smtp_not_configured': return t('delivery.provider.smtp_not_configured');
    case 'smtp_configuration_invalid': return t('delivery.provider.smtp_configuration_invalid');
    case 'telegram_not_configured': return t('delivery.provider.telegram_not_configured');
    case 'telegram_configuration_invalid': return t('delivery.provider.telegram_configuration_invalid');
    case 'whatsapp_not_configured': return t('delivery.provider.whatsapp_not_configured');
    case 'whatsapp_configuration_invalid': return t('delivery.provider.whatsapp_configuration_invalid');
    case 'public_url_missing': return t('delivery.provider.public_url_missing');
    case 'public_url_invalid': return t('delivery.provider.public_url_invalid');
    default: return t('delivery.provider.default');
  }
}

function requiredVariables(channel: DeliveryChannel, provider?: DeliveryProvider): string[] {
  if (provider?.configurationError === 'public_url_missing' || provider?.configurationError === 'public_url_invalid') {
    return ['ROUTEGATE_PUBLIC_URL'];
  }

  switch (channel) {
    case 'telegram':
      return ['ROUTEGATE_TELEGRAM_BOT_TOKEN'];
    case 'whatsapp':
      return [
        'ROUTEGATE_WHATSAPP_ACCESS_TOKEN',
        'ROUTEGATE_WHATSAPP_PHONE_NUMBER_ID',
        'ROUTEGATE_WHATSAPP_GRAPH_API_VERSION',
        'ROUTEGATE_WHATSAPP_TEMPLATE_VPN_ACCESS',
        'ROUTEGATE_WHATSAPP_TEMPLATE_VPN_ACCESS_REISSUED',
        'ROUTEGATE_WHATSAPP_LANGUAGE_EN',
        'ROUTEGATE_WHATSAPP_LANGUAGE_RU',
      ];
    default:
      return [
        'ROUTEGATE_SMTP_HOST',
        'ROUTEGATE_SMTP_PORT',
        'ROUTEGATE_SMTP_USERNAME',
        'ROUTEGATE_SMTP_PASSWORD',
        'ROUTEGATE_SMTP_FROM_ADDRESS',
        'ROUTEGATE_SMTP_FROM_NAME',
        'ROUTEGATE_SMTP_TLS_MODE',
      ];
  }
}

function parseFocusedChannel(value: string | null): DeliveryChannel | null {
  return value === 'email' || value === 'telegram' || value === 'whatsapp' ? value : null;
}

export function DeliverySettingsPanel() {
  const [searchParams] = useSearchParams();
  const sectionRef = useRef<HTMLElement | null>(null);
  const focusedChannel = parseFocusedChannel(searchParams.get('channel'));
  const shouldFocus = searchParams.get('focus') === 'delivery';
  const providersQuery = useQuery({
    queryKey: ['delivery-providers'],
    queryFn: getDeliveryProviders,
  });

  const providersByChannel = useMemo(() => {
    const result = new Map<DeliveryChannel, DeliveryProvider>();
    for (const provider of providersQuery.data?.items ?? []) {
      if (provider.channel === 'email' || provider.channel === 'telegram' || provider.channel === 'whatsapp') {
        result.set(provider.channel, provider);
      }
    }
    return result;
  }, [providersQuery.data]);

  useEffect(() => {
    if (!shouldFocus) return;
    sectionRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, [shouldFocus]);

  return (
    <section className="panel settings-panel settings-delivery-panel" id="delivery-channels" ref={sectionRef}>
      <div className="settings-panel-heading">
        <div>
          <div className="panel-title">{t('delivery.settingsTitle')}</div>
          <p className="panel-subtitle">{t('delivery.settingsSubtitle')}</p>
        </div>
        <button
          className="small-button"
          type="button"
          disabled={providersQuery.isFetching}
          onClick={() => void providersQuery.refetch()}
        >
          {providersQuery.isFetching ? t('delivery.settingsRechecking') : t('delivery.settingsRecheck')}
        </button>
      </div>

      <div className="settings-delivery-security-note">{t('delivery.settingsSecurityNote')}</div>

      {providersQuery.isLoading && <p className="empty-state">{t('delivery.providerLoading')}</p>}
      {providersQuery.isError && <div className="form-message form-message-error">{t('delivery.settingsLoadError')}</div>}

      {!providersQuery.isLoading && !providersQuery.isError && (
        <div className="settings-delivery-grid">
          {channels.map((channel) => {
            const provider = providersByChannel.get(channel);
            const ready = provider?.ready === true;
            const isFocused = shouldFocus && focusedChannel === channel;
            const variables = requiredVariables(channel, provider);
            const publicUrlProblem = provider?.configurationError === 'public_url_missing'
              || provider?.configurationError === 'public_url_invalid';

            return (
              <article
                className={`settings-delivery-card${isFocused ? ' settings-delivery-card-focused' : ''}`}
                key={channel}
              >
                <div className="settings-delivery-card-header">
                  <h3>{channelLabel(channel)}</h3>
                  <span className={`settings-delivery-status ${ready ? 'settings-delivery-status-ready' : 'settings-delivery-status-attention'}`}>
                    {ready ? t('delivery.settingsReady') : t('delivery.settingsNeedsConfiguration')}
                  </span>
                </div>

                <p className="settings-delivery-reason" aria-live="polite">
                  {ready ? (
                    channel === 'email'
                      ? t('delivery.emailProviderReady')
                      : channel === 'telegram'
                        ? t('delivery.telegramProviderReady')
                        : t('delivery.whatsappProviderReady')
                  ) : providerReason(provider?.configurationError)}
                </p>

                {ready ? (
                  <p className="settings-delivery-safe-state">{t('delivery.settingsSecretHidden')}</p>
                ) : (
                  <div className="settings-delivery-setup">
                    {publicUrlProblem && <p>{t('delivery.settingsPublicUrlHint')}</p>}
                    <p>{t('delivery.settingsEditManagerEnv')}</p>
                    <div className="settings-delivery-variable-list">
                      {variables.map((variable) => <code key={variable}>{variable}</code>)}
                    </div>
                    <p>{t('delivery.settingsRestartManager')}</p>
                    <code className="settings-delivery-command">{t('delivery.settingsRestartCommand')}</code>
                  </div>
                )}
              </article>
            );
          })}
        </div>
      )}
    </section>
  );
}
