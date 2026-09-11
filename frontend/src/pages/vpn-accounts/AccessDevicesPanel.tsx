import { type FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createVpnAccountDevice,
  listVpnAccountDevices,
  revokeVpnAccountDevice,
  rotateVpnAccountDeviceToken,
  updateVpnAccountDevice,
  type DeviceClientType,
  type DevicePlatform,
  type VpnAccountDeviceAccess,
} from '../../entities/vpnAccount/api/vpnAccountDeviceApi';
import { getCurrentLocale, t } from '../../shared/i18n/i18n';
import { CollapsiblePanelHeaderTitles } from '../../shared/ui/CollapsiblePanelHeader';
import { SubscriptionQrDialog } from '../../shared/ui/SubscriptionQrDialog';
import './access-devices.css';

type RevealedAccess = {
  subscriptionUrl: string;
  tokenPreview: string;
  expiresAt?: string | null;
};

function getErrorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() !== '' ? error.message : fallback;
}

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      title: 'Доступ и устройства',
      subtitle: 'У каждого устройства — своя ссылка/QR доступа RouteGate. Ссылку можно отозвать или обновить отдельно, не затрагивая другие устройства.',
      loading: 'Загрузка устройств...',
      loadError: 'Не удалось загрузить устройства.',
      empty: 'У этого аккаунта пока нет устройств.',
      addDevice: '+ Добавить устройство',
      cancel: 'Отмена',
      deviceName: 'Название устройства',
      deviceNamePlaceholder: 'Например: iPhone, Рабочий ноутбук',
      client: 'VPN-клиент',
      platform: 'Платформа',
      create: 'Создать доступ',
      creating: 'Создаём...',
      createError: 'Не удалось создать устройство.',
      showQr: 'Показать QR',
      copyLink: 'Копировать ссылку',
      copied: 'Скопировано',
      rotate: 'Обновить ссылку',
      rotating: 'Обновляем...',
      rotateError: 'Не удалось обновить ссылку.',
      revoke: 'Отозвать',
      revoking: 'Отзываем...',
      revokeConfirm: 'Отозвать доступ этого устройства? Ссылка перестанет работать немедленно.',
      revokeError: 'Не удалось отозвать устройство.',
      rename: 'Переименовать',
      save: 'Сохранить',
      renameError: 'Не удалось переименовать устройство.',
      hiddenLinkTitle: 'Ссылка скрыта из соображений безопасности',
      hiddenLinkHint: 'RouteGate хранит только хеш токена и не может показать ссылку снова. Обновите ссылку, чтобы получить новую.',
      tokenPreview: 'Текущий токен',
      newAccessTitle: 'Новый доступ создан',
      newAccessHint: 'Скопируйте или отсканируйте сейчас — после закрытия этой страницы ссылку повторно показать не получится (только обновить).',
      qrTitle: 'QR-код доступа RouteGate',
      revokedSummary: (count: number) => `Отозванные устройства (${count})`,
      revokedAt: (date: string) => `Отозвано: ${date}`,
      full: t('clientCompatibility.full'),
      setup: t('clientCompatibility.setup'),
      generic: t('clientCompatibility.connectionOnly'),
      hiddify: 'Hiddify',
      v2rayn: 'v2rayN',
      v2rayng: 'v2rayNG',
      genericClient: 'Другой клиент',
      windows: 'Windows',
      ios: 'iPhone / iPad',
      android: 'Android',
      macos: 'macOS',
      linux: 'Linux',
      other: 'Другое',
      recommended: 'Рекомендуется',
    } as const;
  }

  return {
    title: 'Access & Devices',
    subtitle: 'Every device gets its own RouteGate access link/QR. Each link can be rotated or revoked independently without affecting other devices.',
    loading: 'Loading devices...',
    loadError: 'Could not load devices.',
    empty: 'This account has no devices yet.',
    addDevice: '+ Add device',
    cancel: 'Cancel',
    deviceName: 'Device name',
    deviceNamePlaceholder: 'For example: iPhone, Work laptop',
    client: 'VPN client',
    platform: 'Platform',
    create: 'Create access',
    creating: 'Creating...',
    createError: 'Failed to create the device.',
    showQr: 'Show QR',
    copyLink: 'Copy link',
    copied: 'Copied',
    rotate: 'Rotate link',
    rotating: 'Rotating...',
    rotateError: 'Failed to rotate the link.',
    revoke: 'Revoke',
    revoking: 'Revoking...',
    revokeConfirm: 'Revoke this device’s access? The link will stop working immediately.',
    revokeError: 'Failed to revoke the device.',
    rename: 'Rename',
    save: 'Save',
    renameError: 'Failed to rename the device.',
    hiddenLinkTitle: 'Link hidden for security',
    hiddenLinkHint: 'RouteGate only stores the token hash and cannot show the link again. Rotate to get a new one.',
    tokenPreview: 'Current token',
    newAccessTitle: 'New access created',
    newAccessHint: 'Copy or scan it now — once you leave this page the link cannot be shown again (only rotated).',
    qrTitle: 'RouteGate access QR code',
    revokedSummary: (count: number) => `Revoked devices (${count})`,
    revokedAt: (date: string) => `Revoked: ${date}`,
    full: t('clientCompatibility.full'),
    setup: t('clientCompatibility.setup'),
    generic: t('clientCompatibility.connectionOnly'),
    hiddify: 'Hiddify',
    v2rayn: 'v2rayN',
    v2rayng: 'v2rayNG',
    genericClient: 'Generic client',
    windows: 'Windows',
    ios: 'iPhone / iPad',
    android: 'Android',
    macos: 'macOS',
    linux: 'Linux',
    other: 'Other',
    recommended: 'Recommended',
  } as const;
}

type Copy = ReturnType<typeof getCopy>;

function clientTypeLabel(clientType: DeviceClientType, copy: Copy): string {
  switch (clientType) {
    case 'hiddify': return copy.hiddify;
    case 'v2rayn': return copy.v2rayn;
    case 'v2rayng': return copy.v2rayng;
    default: return copy.genericClient;
  }
}

function platformLabel(deviceType: DevicePlatform, copy: Copy): string {
  switch (deviceType) {
    case 'windows': return copy.windows;
    case 'ios': return copy.ios;
    case 'android': return copy.android;
    case 'macos': return copy.macos;
    case 'linux': return copy.linux;
    default: return copy.other;
  }
}

function compatibilityLabel(status: string, copy: Copy): string {
  if (status === 'full_smart_routing') return copy.full;
  if (status === 'client_setup_required' || status === 'partial_compatibility') return copy.setup;
  return copy.generic;
}

function compatibilityClass(status: string): string {
  if (status === 'full_smart_routing') return 'vpn-access-device-compat-full';
  if (status === 'client_setup_required' || status === 'partial_compatibility') return 'vpn-access-device-compat-setup';
  return 'vpn-access-device-compat-generic';
}

function formatDate(value?: string | null): string {
  if (!value) return t('common.notAvailable');
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

export function AccessDevicesPanel({ accountId }: { accountId: string }) {
  const copy = getCopy();
  const queryClient = useQueryClient();
  const [isOpen, setIsOpen] = useState(true);
  const [isAddOpen, setIsAddOpen] = useState(false);
  const [name, setName] = useState('');
  const [clientType, setClientType] = useState<DeviceClientType>('hiddify');
  const [deviceType, setDeviceType] = useState<DevicePlatform>('other');
  const [revealed, setRevealed] = useState<Record<string, RevealedAccess>>({});
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [qrDeviceId, setQrDeviceId] = useState<string | null>(null);
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');

  useEffect(() => {
    setRevealed({});
    setIsAddOpen(false);
    setRenamingId(null);
  }, [accountId]);

  const devicesQuery = useQuery({
    queryKey: ['vpn-account-devices', accountId],
    queryFn: () => listVpnAccountDevices(accountId),
  });

  async function refresh() {
    await queryClient.invalidateQueries({ queryKey: ['vpn-account-devices', accountId] });
  }

  const createMutation = useMutation({
    mutationFn: () => createVpnAccountDevice(accountId, {
      name: name.trim() || 'Device',
      clientType,
      deviceType,
    }),
    onSuccess: async (response) => {
      setRevealed((previous) => ({
        ...previous,
        [response.device.id]: {
          subscriptionUrl: response.subscriptionUrl,
          tokenPreview: response.tokenPreview,
          expiresAt: response.expiresAt,
        },
      }));
      setName('');
      setClientType('hiddify');
      setDeviceType('other');
      setIsAddOpen(false);
      await refresh();
    },
  });

  const rotateMutation = useMutation({
    mutationFn: (deviceId: string) => rotateVpnAccountDeviceToken(accountId, deviceId),
    onSuccess: async (response) => {
      setRevealed((previous) => ({
        ...previous,
        [response.device.id]: {
          subscriptionUrl: response.subscriptionUrl,
          tokenPreview: response.tokenPreview,
          expiresAt: response.expiresAt,
        },
      }));
      await refresh();
    },
  });

  const revokeMutation = useMutation({
    mutationFn: (deviceId: string) => revokeVpnAccountDevice(accountId, deviceId),
    onSuccess: async (response) => {
      setRevealed((previous) => {
        const next = { ...previous };
        delete next[response.device.id];
        return next;
      });
      if (qrDeviceId === response.device.id) setQrDeviceId(null);
      await refresh();
    },
  });

  const renameMutation = useMutation({
    mutationFn: (deviceId: string) => updateVpnAccountDevice(accountId, deviceId, {
      name: renameValue.trim() || 'Device',
    }),
    onSuccess: async () => {
      setRenamingId(null);
      await refresh();
    },
  });

  const copyLink = async (deviceId: string, url: string) => {
    if (!navigator.clipboard) return;
    await navigator.clipboard.writeText(url);
    setCopiedId(deviceId);
    window.setTimeout(() => setCopiedId(null), 1800);
  };

  function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (createMutation.isPending) return;
    createMutation.mutate();
  }

  function startRename(access: VpnAccountDeviceAccess) {
    setRenamingId(access.device.id);
    setRenameValue(access.device.name);
  }

  function handleRevoke(deviceId: string) {
    if (window.confirm(copy.revokeConfirm)) revokeMutation.mutate(deviceId);
  }

  const devices = devicesQuery.data?.items ?? [];
  const activeDevices = devices.filter((access) => access.device.status === 'active');
  const revokedDevices = devices.filter((access) => access.device.status !== 'active');
  const qrAccess = qrDeviceId ? revealed[qrDeviceId] : undefined;

  return (
    <div className="panel feature-detail-panel vpn-access-devices-panel">
      <div className="panel-header">
        <CollapsiblePanelHeaderTitles
          title={copy.title}
          subtitle={copy.subtitle}
          open={isOpen}
          onToggle={() => setIsOpen((value) => !value)}
        />
      </div>

      <div className="panel-collapsible-body" hidden={!isOpen}>
        {devicesQuery.isLoading && <p className="empty-state">{copy.loading}</p>}
        {devicesQuery.isError && <div className="form-message form-message-error">{copy.loadError}</div>}
        {!devicesQuery.isLoading && !devicesQuery.isError && activeDevices.length === 0 && (
          <p className="empty-state">{copy.empty}</p>
        )}

        {activeDevices.length > 0 && (
          <div className="vpn-access-devices-grid">
            {activeDevices.map((access) => {
              const { device, compatibility } = access;
              const reveal = revealed[device.id];
              const isRenaming = renamingId === device.id;
              const isRotatingThis = rotateMutation.isPending && rotateMutation.variables === device.id;
              const isRevokingThis = revokeMutation.isPending && revokeMutation.variables === device.id;

              return (
                <section className="vpn-access-device-card" key={device.id}>
                  <div className="vpn-access-device-card-header">
                    {isRenaming ? (
                      <form
                        className="vpn-access-device-rename-form"
                        onSubmit={(event) => { event.preventDefault(); renameMutation.mutate(device.id); }}
                      >
                        <input value={renameValue} onChange={(event) => setRenameValue(event.target.value)} maxLength={100} autoFocus />
                        <button className="small-button" type="submit" disabled={renameMutation.isPending}>{copy.save}</button>
                        <button className="small-button" type="button" onClick={() => setRenamingId(null)}>{copy.cancel}</button>
                      </form>
                    ) : (
                      <strong>{device.name}</strong>
                    )}
                    <span className={`status-pill ${compatibilityClass(compatibility.status)}`}>
                      {compatibilityLabel(compatibility.status, copy)}
                    </span>
                  </div>

                  <div className="vpn-access-device-meta">
                    <span>{clientTypeLabel(device.clientType, copy)}</span>
                    <span>·</span>
                    <span>{platformLabel(device.deviceType, copy)}</span>
                    {device.clientType === 'hiddify' && <span className="vpn-access-device-recommended">{copy.recommended}</span>}
                  </div>

                  {(compatibility.guidance ?? []).map((item) => (
                    <p className="vpn-access-device-note" key={item}>{item}</p>
                  ))}
                  {(compatibility.limitations ?? []).map((item) => (
                    <p className="vpn-access-device-note vpn-access-device-note-warning" key={item}>{item}</p>
                  ))}

                  {renameMutation.isError && renamingId === null && (
                    <div className="form-message form-message-error">{getErrorMessage(renameMutation.error, copy.renameError)}</div>
                  )}

                  {reveal ? (
                    <>
                      <div className="form-message form-message-success vpn-access-device-reveal">
                        <strong>{copy.newAccessTitle}</strong>
                        <span>{copy.newAccessHint}</span>
                      </div>
                      <code className="vpn-access-device-url">{reveal.subscriptionUrl}</code>
                    </>
                  ) : (
                    <>
                      <div className="form-message form-message-warning vpn-access-device-hidden">
                        <strong>{copy.hiddenLinkTitle}</strong>
                        <span>{copy.hiddenLinkHint}</span>
                      </div>
                      {access.tokenPreview && <code className="vpn-access-device-token-preview">{copy.tokenPreview}: {access.tokenPreview}</code>}
                    </>
                  )}

                  <div className="form-actions">
                    <button
                      className="small-button"
                      type="button"
                      disabled={!reveal}
                      onClick={() => setQrDeviceId(device.id)}
                    >
                      {copy.showQr}
                    </button>
                    <button
                      className="small-button"
                      type="button"
                      disabled={!reveal}
                      onClick={() => reveal && void copyLink(device.id, reveal.subscriptionUrl)}
                    >
                      {copiedId === device.id ? copy.copied : copy.copyLink}
                    </button>
                    <button
                      className="small-button"
                      type="button"
                      disabled={isRotatingThis}
                      onClick={() => rotateMutation.mutate(device.id)}
                    >
                      {isRotatingThis ? copy.rotating : copy.rotate}
                    </button>
                    {!isRenaming && (
                      <button className="small-button" type="button" onClick={() => startRename(access)}>{copy.rename}</button>
                    )}
                    <button
                      className="small-button danger-button"
                      type="button"
                      disabled={isRevokingThis}
                      onClick={() => handleRevoke(device.id)}
                    >
                      {isRevokingThis ? copy.revoking : copy.revoke}
                    </button>
                  </div>
                  {rotateMutation.isError && rotateMutation.variables === device.id && (
                    <div className="form-message form-message-error">{getErrorMessage(rotateMutation.error, copy.rotateError)}</div>
                  )}
                  {revokeMutation.isError && revokeMutation.variables === device.id && (
                    <div className="form-message form-message-error">{getErrorMessage(revokeMutation.error, copy.revokeError)}</div>
                  )}
                </section>
              );
            })}
          </div>
        )}

        {revokedDevices.length > 0 && (
          <details className="vpn-access-devices-revoked">
            <summary>{copy.revokedSummary(revokedDevices.length)}</summary>
            {revokedDevices.map((access) => (
              <div className="vpn-access-device-revoked-row" key={access.device.id}>
                <span>{access.device.name}</span>
                <span>{clientTypeLabel(access.device.clientType, copy)}</span>
                <span>{copy.revokedAt(formatDate(access.device.revokedAt))}</span>
              </div>
            ))}
          </details>
        )}

        {isAddOpen ? (
          <form className="vpn-access-device-add-form" onSubmit={handleCreate}>
            <div className="vpn-account-create-grid">
              <label className="field">
                <span>{copy.deviceName}</span>
                <input value={name} onChange={(event) => setName(event.target.value)} placeholder={copy.deviceNamePlaceholder} maxLength={100} />
              </label>
              <label className="field">
                <span>{copy.client}</span>
                <select value={clientType} onChange={(event) => setClientType(event.target.value as DeviceClientType)}>
                  <option value="hiddify">{copy.hiddify} · {copy.recommended}</option>
                  <option value="v2rayn">{copy.v2rayn}</option>
                  <option value="v2rayng">{copy.v2rayng}</option>
                  <option value="generic">{copy.genericClient}</option>
                </select>
              </label>
              <label className="field">
                <span>{copy.platform}</span>
                <select value={deviceType} onChange={(event) => setDeviceType(event.target.value as DevicePlatform)}>
                  <option value="ios">{copy.ios}</option>
                  <option value="android">{copy.android}</option>
                  <option value="windows">{copy.windows}</option>
                  <option value="macos">{copy.macos}</option>
                  <option value="linux">{copy.linux}</option>
                  <option value="other">{copy.other}</option>
                </select>
              </label>
            </div>
            {createMutation.isError && (
              <div className="form-message form-message-error">{getErrorMessage(createMutation.error, copy.createError)}</div>
            )}
            <div className="form-actions">
              <button className="primary-button" type="submit" disabled={createMutation.isPending}>
                {createMutation.isPending ? copy.creating : copy.create}
              </button>
              <button className="small-button" type="button" onClick={() => setIsAddOpen(false)}>{copy.cancel}</button>
            </div>
          </form>
        ) : (
          <div className="form-actions">
            <button className="small-button" type="button" onClick={() => setIsAddOpen(true)}>{copy.addDevice}</button>
          </div>
        )}
      </div>

      <SubscriptionQrDialog
        isOpen={Boolean(qrDeviceId && qrAccess)}
        title={copy.qrTitle}
        onClose={() => setQrDeviceId(null)}
        qrText={qrAccess?.subscriptionUrl}
        url={qrAccess?.subscriptionUrl}
        urlLabel={copy.copyLink}
        onCopyQrText={() => qrDeviceId && qrAccess && void copyLink(qrDeviceId, qrAccess.subscriptionUrl)}
        copyQrLabel={copy.copyLink}
        copyCopiedLabel={copy.copied}
        copied={copiedId === qrDeviceId}
        closeLabel={t('clientCompatibility.close')}
      />
    </div>
  );
}
