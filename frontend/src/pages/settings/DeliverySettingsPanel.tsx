import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import {
  getDeliveryProviderSettings,
  providerNameForChannel,
  removeDeliveryProviderSettings,
  saveDeliveryProviderSettings,
  testDeliveryProviderSettings,
  type DeliveryChannel,
  type DeliveryProviderConfig,
  type DeliveryProviderName,
  type DeliveryProviderSettings,
  type SaveDeliveryProviderSettingsRequest,
} from '../../entities/delivery/api/deliveryApi';
import { ApiError } from '../../shared/api/client';
import { t } from '../../shared/i18n/i18n';
import './DeliverySettingsPanel.css';

const channels: DeliveryChannel[] = ['email', 'telegram'];

function channelLabel(channel: DeliveryChannel): string {
  switch (channel) {
    case 'telegram': return t('delivery.telegram');
    default: return t('delivery.email');
  }
}

function providerReason(code?: string): string {
  switch (code) {
    case 'smtp_not_configured': return t('delivery.provider.smtp_not_configured');
    case 'smtp_configuration_invalid': return t('delivery.provider.smtp_configuration_invalid');
    case 'telegram_not_configured': return t('delivery.provider.telegram_not_configured');
    case 'telegram_configuration_invalid': return t('delivery.provider.telegram_configuration_invalid');
    case 'delivery_provider_disabled': return t('delivery.provider.disabled');
    case 'secret_store_unavailable': return t('delivery.provider.secret_store_unavailable');
    case 'provider_secret_decryption_failed': return t('delivery.provider.secret_decryption_failed');
    case 'public_url_missing': return t('delivery.provider.public_url_missing');
    case 'public_url_invalid': return t('delivery.provider.public_url_invalid');
    default: return t('delivery.provider.default');
  }
}

function providerTestReason(code?: string): string {
  switch (code) {
    case 'smtp_not_configured':
    case 'smtp_configuration_invalid':
    case 'telegram_not_configured':
    case 'telegram_configuration_invalid':
      return providerReason(code);
    case 'smtp_connect_failed': return t('settings.smtpTestConnectFailed');
    case 'smtp_auth_failed': return t('settings.smtpTestAuthFailed');
    case 'smtp_starttls_unavailable': return t('settings.smtpTestStartTLSUnavailable');
    case 'smtp_tls_failed': return t('settings.smtpTestTLSFailed');
    default: return t('delivery.settingsTestFailed');
  }
}

function parseFocusedChannel(value: string | null): DeliveryChannel | null {
  return value === 'email' || value === 'telegram' ? value : null;
}

function sourceLabel(source?: string): string {
  switch (source) {
    case 'managed': return t('delivery.settingsSourceManaged');
    case 'environment': return t('delivery.settingsSourceEnvironment');
    default: return t('delivery.settingsSourceNone');
  }
}

function asString(config: DeliveryProviderConfig, key: string, fallback = ''): string {
  const value = config[key];
  return typeof value === 'string' ? value : typeof value === 'number' ? String(value) : fallback;
}

function asNumber(config: DeliveryProviderConfig, key: string, fallback: number): number {
  const value = config[key];
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string') {
    const parsed = Number.parseInt(value, 10);
    if (Number.isFinite(parsed)) return parsed;
  }
  return fallback;
}

function safeMutationError(error: unknown): string {
  if (error instanceof ApiError && error.code) {
    switch (error.code) {
      case 'smtp_not_configured':
      case 'smtp_configuration_invalid':
      case 'telegram_not_configured':
      case 'telegram_configuration_invalid':
      case 'secret_store_unavailable':
      case 'provider_secret_decryption_failed':
        return providerReason(error.code);
      default:
        break;
    }
  }
  return t('delivery.settingsSaveError');
}

export function DeliverySettingsPanel() {
  const queryClient = useQueryClient();
  const [searchParams] = useSearchParams();
  const sectionRef = useRef<HTMLElement | null>(null);
  const focusedChannel = parseFocusedChannel(searchParams.get('channel'));
  const shouldFocus = searchParams.get('focus') === 'delivery';
  const [openChannel, setOpenChannel] = useState<DeliveryChannel | null>(shouldFocus ? focusedChannel : null);
  const returnTo = searchParams.get('returnTo');

  useEffect(() => {
    if (!shouldFocus) return;
    sectionRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    if (focusedChannel) setOpenChannel(focusedChannel);
  }, [focusedChannel, shouldFocus]);

  async function recheckAll() {
    await queryClient.invalidateQueries({ queryKey: ['delivery-provider-settings'] });
    await queryClient.invalidateQueries({ queryKey: ['delivery-providers'] });
  }

  return (
    <section className="panel settings-panel settings-delivery-panel" id="delivery-channels" ref={sectionRef}>
      <div className="settings-panel-heading">
        <div>
          <div className="panel-title">{t('delivery.settingsTitle')}</div>
          <p className="panel-subtitle">{t('delivery.settingsSubtitle')}</p>
        </div>
        <button className="small-button" type="button" onClick={() => void recheckAll()}>
          {t('delivery.settingsRecheck')}
        </button>
      </div>

      <div className="settings-delivery-security-note">{t('delivery.settingsSecurityNote')}</div>

      <div className="settings-delivery-grid">
        {channels.map((channel) => (
          <DeliveryChannelCard
            channel={channel}
            focused={shouldFocus && focusedChannel === channel}
            key={channel}
            open={openChannel === channel}
            onToggle={() => setOpenChannel((current) => current === channel ? null : channel)}
          />
        ))}
      </div>

      {returnTo && (
        <div className="settings-delivery-return">
          <Link className="small-button" to={returnTo}>{t('delivery.settingsReturnToAccess')}</Link>
        </div>
      )}
    </section>
  );
}

function DeliveryChannelCard({
  channel,
  focused,
  open,
  onToggle,
}: {
  channel: DeliveryChannel;
  focused: boolean;
  open: boolean;
  onToggle: () => void;
}) {
  const provider = providerNameForChannel(channel);
  const settingsQuery = useQuery({
    queryKey: ['delivery-provider-settings', provider],
    queryFn: () => getDeliveryProviderSettings(provider),
  });
  const settings = settingsQuery.data;
  const ready = settings?.ready === true;

  return (
    <article className={`settings-delivery-card${focused ? ' settings-delivery-card-focused' : ''}${open ? ' settings-delivery-card-open' : ''}`}>
      <div className="settings-delivery-card-header">
        <div>
          <h3>{channelLabel(channel)}</h3>
          {settings && <small>{sourceLabel(settings.source)}</small>}
        </div>
        <span className={`settings-delivery-status ${ready ? 'settings-delivery-status-ready' : 'settings-delivery-status-attention'}`}>
          {ready ? t('delivery.settingsReady') : t('delivery.settingsNeedsConfiguration')}
        </span>
      </div>

      {settingsQuery.isLoading && <p className="empty-state">{t('delivery.settingsLoadingChannel')}</p>}
      {settingsQuery.isError && <div className="form-message form-message-error">{t('delivery.settingsLoadError')}</div>}
      {settings && (
        <>
          <p className="settings-delivery-reason" aria-live="polite">
            {ready
              ? channel === 'email'
                ? t('delivery.emailProviderReady')
                : t('delivery.telegramProviderReady')
              : providerReason(settings.configurationError)}
          </p>
          <div className="settings-delivery-card-actions">
            <button className={open ? 'small-button' : 'primary-button'} type="button" onClick={onToggle}>
              {open
                ? t('delivery.settingsClose')
                : settings.source === 'managed'
                  ? t('delivery.settingsEdit')
                  : t('delivery.settingsConfigure')}
            </button>
          </div>
          {open && <ProviderSettingsEditor channel={channel} provider={provider} settings={settings} />}
        </>
      )}
    </article>
  );
}

function ProviderSettingsEditor({
  channel,
  provider,
  settings,
}: {
  channel: DeliveryChannel;
  provider: DeliveryProviderName;
  settings: DeliveryProviderSettings;
}) {
  const queryClient = useQueryClient();
  const [enabled, setEnabled] = useState(settings.enabled);
  const [secret, setSecret] = useState('');
  const [fields, setFields] = useState<Record<string, string>>({});
  const [testResult, setTestResult] = useState<'idle' | 'success' | 'failure'>('idle');
  const [testErrorCode, setTestErrorCode] = useState('');
  const [savedNotice, setSavedNotice] = useState(false);

  useEffect(() => {
    setEnabled(settings.enabled);
    setSecret('');
    setSavedNotice(false);
    setTestResult('idle');
    setTestErrorCode('');
    if (provider === 'smtp') {
      setFields({
        host: asString(settings.config, 'host'),
        port: String(asNumber(settings.config, 'port', 587)),
        username: asString(settings.config, 'username'),
        fromAddress: asString(settings.config, 'fromAddress'),
        fromName: asString(settings.config, 'fromName', 'RouteGate'),
        tlsMode: asString(settings.config, 'tlsMode', 'starttls'),
      });
    } else {
      setFields({});
    }
  }, [provider, settings]);

  function updateField(key: string, value: string) {
    setFields((current) => ({ ...current, [key]: value }));
    setSavedNotice(false);
    setTestResult('idle');
  }

  const request = useMemo<SaveDeliveryProviderSettingsRequest>(() => {
    let config: DeliveryProviderConfig;
    if (provider === 'smtp') {
      config = {
        host: fields.host ?? '',
        port: Number.parseInt(fields.port ?? '587', 10) || 587,
        username: fields.username ?? '',
        fromAddress: fields.fromAddress ?? '',
        fromName: fields.fromName ?? 'RouteGate',
        tlsMode: fields.tlsMode ?? 'starttls',
      };
    } else {
      config = {};
    }

    const result: SaveDeliveryProviderSettingsRequest = { enabled, config };
    if (secret !== '') {
      result.secret = secret;
    } else if (provider === 'smtp' && (fields.username ?? '').trim() === '') {
      result.secret = '';
    }
    return result;
  }, [enabled, fields, provider, secret]);

  const saveMutation = useMutation({
    mutationFn: () => saveDeliveryProviderSettings(provider, request),
    onSuccess: async () => {
      setSecret('');
      setSavedNotice(true);
      setTestResult('idle');
      await queryClient.invalidateQueries({ queryKey: ['delivery-provider-settings', provider] });
      await queryClient.invalidateQueries({ queryKey: ['delivery-providers'] });
    },
  });

  const testMutation = useMutation({
    mutationFn: () => testDeliveryProviderSettings(provider, request),
    onSuccess: (result) => {
      setTestResult(result.ok ? 'success' : 'failure');
      setTestErrorCode(result.errorCode ?? '');
    },
    onError: () => {
      setTestResult('failure');
      setTestErrorCode('');
    },
  });

  const removeMutation = useMutation({
    mutationFn: () => removeDeliveryProviderSettings(provider),
    onSuccess: async () => {
      setSecret('');
      setSavedNotice(false);
      await queryClient.invalidateQueries({ queryKey: ['delivery-provider-settings', provider] });
      await queryClient.invalidateQueries({ queryKey: ['delivery-providers'] });
    },
  });

  function removeManagedSettings() {
    if (!window.confirm(t('delivery.settingsRemoveConfirm', { channel: channelLabel(channel) }))) return;
    removeMutation.mutate();
  }

  return (
    <div className="settings-delivery-editor">
      {settings.source === 'environment' && (
        <div className="form-message form-message-warning">{t('delivery.settingsLegacyImportHint')}</div>
      )}

      <label className="settings-delivery-enabled">
        <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
        <span>{t('delivery.settingsEnabled')}</span>
      </label>

      {provider === 'smtp' && (
        <>
          <div className="settings-delivery-form-grid">
            <TextField label={t('delivery.settingsSmtpHost')} value={fields.host ?? ''} onChange={(value) => updateField('host', value)} placeholder="smtp.example.com" />
            <TextField label={t('delivery.settingsSmtpPort')} value={fields.port ?? '587'} onChange={(value) => updateField('port', value)} inputMode="numeric" placeholder="587" />
            <TextField label={t('delivery.settingsSmtpUsername')} value={fields.username ?? ''} onChange={(value) => updateField('username', value)} placeholder="routegate@example.com" />
            <SecretField configured={settings.secretConfigured} label={t('delivery.settingsSmtpPassword')} value={secret} onChange={setSecret} />
            <TextField label={t('delivery.settingsSmtpFromAddress')} value={fields.fromAddress ?? ''} onChange={(value) => updateField('fromAddress', value)} placeholder="routegate@example.com" />
            <TextField label={t('delivery.settingsSmtpFromName')} value={fields.fromName ?? 'RouteGate'} onChange={(value) => updateField('fromName', value)} placeholder="RouteGate" />
            <label className="field">
              <span>{t('delivery.settingsSmtpSecurity')}</span>
              <select value={fields.tlsMode ?? 'starttls'} onChange={(event) => updateField('tlsMode', event.target.value)}>
                <option value="starttls">STARTTLS</option>
                <option value="tls">TLS</option>
              </select>
            </label>
          </div>
          <p className="settings-delivery-field-hint">{t('settings.smtpEgressHint')}</p>
        </>
      )}

      {provider === 'telegram' && (
        <div className="settings-delivery-form-grid settings-delivery-form-grid-single">
          <SecretField configured={settings.secretConfigured} label={t('delivery.settingsTelegramToken')} value={secret} onChange={setSecret} placeholder="123456789:AA..." />
          <p className="settings-delivery-field-hint">{t('delivery.settingsTelegramTokenHint')}</p>
        </div>
      )}

      <div className="settings-delivery-secret-note">{t('delivery.settingsSecretWriteOnly')}</div>

      {testResult === 'success' && <div className="form-message form-message-success">{t('delivery.settingsTestSuccess')}</div>}
      {testResult === 'failure' && (
        <div className="form-message form-message-error">{providerTestReason(testErrorCode)}</div>
      )}
      {savedNotice && <div className="form-message form-message-success">{t('delivery.settingsSaved')}</div>}
      {saveMutation.isError && <div className="form-message form-message-error">{safeMutationError(saveMutation.error)}</div>}
      {removeMutation.isError && <div className="form-message form-message-error">{t('delivery.settingsRemoveError')}</div>}

      <div className="form-actions settings-delivery-editor-actions">
        <button className="small-button" type="button" disabled={testMutation.isPending || saveMutation.isPending} onClick={() => testMutation.mutate()}>
          {testMutation.isPending ? t('delivery.settingsTesting') : t('delivery.settingsTest')}
        </button>
        <button className="primary-button" type="button" disabled={saveMutation.isPending || testMutation.isPending} onClick={() => saveMutation.mutate()}>
          {saveMutation.isPending ? t('delivery.settingsSaving') : t('delivery.settingsSave')}
        </button>
        {settings.source === 'managed' && (
          <button className="small-button settings-delivery-remove" type="button" disabled={removeMutation.isPending} onClick={removeManagedSettings}>
            {removeMutation.isPending ? t('delivery.settingsRemoving') : t('delivery.settingsRemove')}
          </button>
        )}
      </div>
    </div>
  );
}

function TextField({
  label,
  value,
  onChange,
  placeholder,
  inputMode,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  inputMode?: 'numeric' | 'text';
}) {
  return (
    <label className="field">
      <span>{label}</span>
      <input inputMode={inputMode} value={value} placeholder={placeholder} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function SecretField({
  configured,
  label,
  value,
  onChange,
  placeholder,
}: {
  configured: boolean;
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  return (
    <label className="field">
      <span>{label}</span>
      <input
        autoComplete="new-password"
        type="password"
        value={value}
        placeholder={configured ? t('delivery.settingsSecretStoredPlaceholder') : placeholder}
        onChange={(event) => onChange(event.target.value)}
      />
      {configured && <small>{t('delivery.settingsSecretStoredHint')}</small>}
    </label>
  );
}