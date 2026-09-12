import { type FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSearchParams } from 'react-router-dom';
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
import {
  getVpnAccountLegacySubscriptionAccess,
  revokeVpnAccountSubscriptionToken,
  rotateVpnAccountSubscriptionToken,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { t } from '../../shared/i18n/i18n';
import { clientCompatibilityGuidanceKey, clientCompatibilityLimitationKey } from '../../shared/i18n/clientCompatibilityTranslations';
import { CollapsiblePanelHeaderTitles } from '../../shared/ui/CollapsiblePanelHeader';
import { SubscriptionQrDialog } from '../../shared/ui/SubscriptionQrDialog';
import { DeviceSendComposer } from './DeviceSendComposer';
import './access-devices.css';

type RevealedAccess = {
  subscriptionUrl: string;
  tokenPreview: string;
  expiresAt?: string | null;
};

function getErrorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() !== '' ? error.message : fallback;
}

function clientTypeLabel(clientType: DeviceClientType): string {
  switch (clientType) {
    case 'hiddify': return 'Hiddify';
    case 'v2rayn': return 'v2rayN';
    case 'v2rayng': return 'v2rayNG';
    default: return t('accessDevices.genericClient');
  }
}

function platformLabel(deviceType: DevicePlatform): string {
  switch (deviceType) {
    case 'windows': return t('accessDevices.platformWindows');
    case 'ios': return t('accessDevices.platformIos');
    case 'android': return t('accessDevices.platformAndroid');
    case 'macos': return t('accessDevices.platformMacos');
    case 'linux': return t('accessDevices.platformLinux');
    default: return t('accessDevices.platformOther');
  }
}

function compatibilityLabel(status: string): string {
  if (status === 'full_smart_routing') return t('clientCompatibility.full');
  if (status === 'client_setup_required' || status === 'partial_compatibility') return t('clientCompatibility.setup');
  return t('clientCompatibility.connectionOnly');
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
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const [isOpen, setIsOpen] = useState(true);
  const [isAddOpen, setIsAddOpen] = useState(searchParams.get('addDevice') === '1');
  const [name, setName] = useState('');
  const [clientType, setClientType] = useState<DeviceClientType>('hiddify');
  const [deviceType, setDeviceType] = useState<DevicePlatform>('other');
  const [revealed, setRevealed] = useState<Record<string, RevealedAccess>>({});
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [qrDeviceId, setQrDeviceId] = useState<string | null>(null);
  const [sendDeviceId, setSendDeviceId] = useState<string | null>(null);
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [legacyRevealed, setLegacyRevealed] = useState<RevealedAccess | null>(null);
  const [legacyQrOpen, setLegacyQrOpen] = useState(false);
  // rotateMutation/revokeMutation below are each a single shared useMutation
  // instance reused by every device row via .mutate(deviceId). TanStack
  // Query's own reactive `isPending`/`variables` reflect only the most
  // recently invoked call, not every in-flight call - so tracking "is THIS
  // device's rotate/revoke in flight" via those two fields breaks the moment
  // two different devices are rotated/revoked in overlapping windows (the
  // first device's button would incorrectly re-enable while its own request
  // is still in flight). Track pending device IDs explicitly instead, via
  // onMutate/onSettled, which fire per-call with that call's own variables
  // regardless of what else is in flight.
  const [pendingRotateIds, setPendingRotateIds] = useState<ReadonlySet<string>>(new Set());
  const [pendingRevokeIds, setPendingRevokeIds] = useState<ReadonlySet<string>>(new Set());
  // Same reasoning as above: per-device error state, since the shared
  // mutation's own `.error`/`.isError` only ever reflect the most recently
  // invoked call and would otherwise make one device's failure vanish (or
  // appear to belong to a different device) the moment another device's
  // rotate/revoke is invoked.
  const [rotateErrors, setRotateErrors] = useState<Record<string, unknown>>({});
  const [revokeErrors, setRevokeErrors] = useState<Record<string, unknown>>({});

  useEffect(() => {
    setRevealed({});
    setRenamingId(null);
    setSendDeviceId(null);
    setLegacyRevealed(null);
    setLegacyQrOpen(false);
    setRotateErrors({});
    setRevokeErrors({});
  }, [accountId]);

  useEffect(() => {
    if (searchParams.get('addDevice') === '1') {
      setIsAddOpen(true);
      setIsOpen(true);
    }
  }, [searchParams]);

  function clearAddDeviceParam() {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      next.delete('addDevice');
      return next;
    }, { replace: true });
  }

  const devicesQuery = useQuery({
    queryKey: ['vpn-account-devices', accountId],
    queryFn: () => listVpnAccountDevices(accountId),
  });

  const legacyAccessQuery = useQuery({
    queryKey: ['vpn-account-legacy-access', accountId],
    queryFn: () => getVpnAccountLegacySubscriptionAccess(accountId),
  });

  async function refresh() {
    await queryClient.invalidateQueries({ queryKey: ['vpn-account-devices', accountId] });
  }

  async function refreshLegacyAccess() {
    await queryClient.invalidateQueries({ queryKey: ['vpn-account-legacy-access', accountId] });
  }

  const legacyRotateMutation = useMutation({
    mutationFn: () => rotateVpnAccountSubscriptionToken(accountId),
    onSuccess: async (response) => {
      setLegacyRevealed({
        subscriptionUrl: response.subscriptionUrl,
        tokenPreview: '',
        expiresAt: response.expiresAt,
      });
      await refreshLegacyAccess();
    },
  });

  const legacyRevokeMutation = useMutation({
    mutationFn: () => revokeVpnAccountSubscriptionToken(accountId),
    onSuccess: async () => {
      setLegacyRevealed(null);
      setLegacyQrOpen(false);
      await refreshLegacyAccess();
    },
  });

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
      clearAddDeviceParam();
      await refresh();
    },
  });

  const rotateMutation = useMutation({
    mutationFn: (deviceId: string) => rotateVpnAccountDeviceToken(accountId, deviceId),
    onMutate: (deviceId) => {
      setPendingRotateIds((previous) => new Set(previous).add(deviceId));
    },
    onSettled: (_data, _error, deviceId) => {
      setPendingRotateIds((previous) => {
        const next = new Set(previous);
        next.delete(deviceId);
        return next;
      });
    },
    onError: (error, deviceId) => {
      setRotateErrors((previous) => ({ ...previous, [deviceId]: error }));
    },
    onSuccess: async (response) => {
      setRotateErrors((previous) => {
        const next = { ...previous };
        delete next[response.device.id];
        return next;
      });
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
    onMutate: (deviceId) => {
      setPendingRevokeIds((previous) => new Set(previous).add(deviceId));
    },
    onSettled: (_data, _error, deviceId) => {
      setPendingRevokeIds((previous) => {
        const next = new Set(previous);
        next.delete(deviceId);
        return next;
      });
    },
    onError: (error, deviceId) => {
      setRevokeErrors((previous) => ({ ...previous, [deviceId]: error }));
    },
    onSuccess: async (response) => {
      setRevokeErrors((previous) => {
        const next = { ...previous };
        delete next[response.device.id];
        return next;
      });
      setRevealed((previous) => {
        const next = { ...previous };
        delete next[response.device.id];
        return next;
      });
      if (qrDeviceId === response.device.id) setQrDeviceId(null);
      if (sendDeviceId === response.device.id) setSendDeviceId(null);
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
    if (window.confirm(t('accessDevices.revokeConfirm'))) revokeMutation.mutate(deviceId);
  }

  function handleLegacyRevoke() {
    if (window.confirm(t('accessDevices.legacyRevokeConfirm'))) legacyRevokeMutation.mutate();
  }

  function toggleSend(deviceId: string) {
    setSendDeviceId((current) => (current === deviceId ? null : deviceId));
  }

  const devices = devicesQuery.data?.items ?? [];
  const activeDevices = devices.filter((access) => access.device.status === 'active');
  const revokedDevices = devices.filter((access) => access.device.status !== 'active');
  const qrAccess = qrDeviceId ? revealed[qrDeviceId] : undefined;

  return (
    <div className="panel feature-detail-panel vpn-access-devices-panel">
      <div className="panel-header">
        <CollapsiblePanelHeaderTitles
          title={t('accessDevices.title')}
          subtitle={t('accessDevices.subtitle')}
          open={isOpen}
          onToggle={() => setIsOpen((value) => !value)}
        />
      </div>

      <div className="panel-collapsible-body" hidden={!isOpen}>
        {devicesQuery.isLoading && <p className="empty-state">{t('accessDevices.loading')}</p>}
        {devicesQuery.isError && <div className="form-message form-message-error">{t('accessDevices.loadError')}</div>}
        {!devicesQuery.isLoading && !devicesQuery.isError && activeDevices.length === 0 && (
          <p className="empty-state">{t('accessDevices.empty')}</p>
        )}

        {legacyAccessQuery.isError && (
          <div className="form-message form-message-error">{t('accessDevices.legacyLoadError')}</div>
        )}
        {legacyAccessQuery.data?.hasActiveToken && (
          <section className="vpn-access-legacy-card">
            <div className="vpn-access-device-card-header">
              <strong>{t('accessDevices.legacyTitle')}</strong>
              <span className="vpn-access-legacy-badge">{t('accessDevices.legacyBadge')}</span>
            </div>
            <p className="vpn-access-device-note">{t('accessDevices.legacyDescription')}</p>
            <div className="vpn-access-device-meta">
              <span>{t('accessDevices.legacyCreatedAt', { date: formatDate(legacyAccessQuery.data.createdAt) })}</span>
              <span>·</span>
              <span>{t('accessDevices.legacyExpiresAt', { date: formatDate(legacyAccessQuery.data.expiresAt) })}</span>
              <span>·</span>
              <span>
                {legacyAccessQuery.data.lastUsedAt
                  ? t('accessDevices.legacyLastUsedAt', { date: formatDate(legacyAccessQuery.data.lastUsedAt) })
                  : t('accessDevices.legacyNeverUsed')}
              </span>
            </div>

            {legacyRevealed ? (
              <>
                <div className="form-message form-message-success vpn-access-device-reveal">
                  <strong>{t('accessDevices.newAccessTitle')}</strong>
                  <span>{t('accessDevices.newAccessHint')}</span>
                </div>
                <code className="vpn-access-device-url">{legacyRevealed.subscriptionUrl}</code>
              </>
            ) : (
              <div className="form-message form-message-warning vpn-access-device-hidden">
                <strong>{t('accessDevices.hiddenLinkTitle')}</strong>
                <span>{t('accessDevices.hiddenLinkHint')}</span>
              </div>
            )}

            <div className="form-actions">
              <button
                className="small-button"
                type="button"
                disabled={!legacyRevealed}
                onClick={() => setLegacyQrOpen(true)}
              >
                {t('accessDevices.showQr')}
              </button>
              <button
                className="small-button"
                type="button"
                disabled={!legacyRevealed}
                onClick={() => legacyRevealed && void copyLink('legacy', legacyRevealed.subscriptionUrl)}
              >
                {copiedId === 'legacy' ? t('clientCompatibility.copied') : t('accessDevices.copyLink')}
              </button>
              <button
                className="small-button"
                type="button"
                disabled={legacyRotateMutation.isPending}
                onClick={() => legacyRotateMutation.mutate()}
              >
                {legacyRotateMutation.isPending ? t('accessDevices.legacyRotating') : t('accessDevices.legacyRotate')}
              </button>
              <button
                className="small-button danger-button"
                type="button"
                disabled={legacyRevokeMutation.isPending}
                onClick={handleLegacyRevoke}
              >
                {legacyRevokeMutation.isPending ? t('accessDevices.legacyRevoking') : t('accessDevices.legacyRevoke')}
              </button>
            </div>
            {legacyRotateMutation.isError && (
              <div className="form-message form-message-error">{getErrorMessage(legacyRotateMutation.error, t('accessDevices.legacyRotateError'))}</div>
            )}
            {legacyRevokeMutation.isError && (
              <div className="form-message form-message-error">{getErrorMessage(legacyRevokeMutation.error, t('accessDevices.legacyRevokeError'))}</div>
            )}
          </section>
        )}

        {activeDevices.length > 0 && (
          <div className="vpn-access-devices-grid">
            {activeDevices.map((access) => {
              const { device, compatibility } = access;
              const reveal = revealed[device.id];
              const isRenaming = renamingId === device.id;
              const isSending = sendDeviceId === device.id;
              const isRotatingThis = pendingRotateIds.has(device.id);
              const isRevokingThis = pendingRevokeIds.has(device.id);

              return (
                <section className="vpn-access-device-card" key={device.id}>
                  <div className="vpn-access-device-card-header">
                    {isRenaming ? (
                      <form
                        className="vpn-access-device-rename-form"
                        onSubmit={(event) => { event.preventDefault(); renameMutation.mutate(device.id); }}
                      >
                        <input value={renameValue} onChange={(event) => setRenameValue(event.target.value)} maxLength={100} autoFocus />
                        <button className="small-button" type="submit" disabled={renameMutation.isPending}>{t('accessDevices.save')}</button>
                        <button className="small-button" type="button" onClick={() => setRenamingId(null)}>{t('common.cancel')}</button>
                      </form>
                    ) : (
                      <strong>{device.name}</strong>
                    )}
                    <span className={`status-pill ${compatibilityClass(compatibility.status)}`}>
                      {compatibilityLabel(compatibility.status)}
                    </span>
                  </div>

                  <div className="vpn-access-device-meta">
                    <span>{clientTypeLabel(device.clientType)}</span>
                    <span>·</span>
                    <span>{platformLabel(device.deviceType)}</span>
                    {device.clientType === 'hiddify' && <span className="vpn-access-device-recommended">{t('accessDevices.recommended')}</span>}
                  </div>

                  {(compatibility.guidanceCodes ?? []).map((code) => {
                    const key = clientCompatibilityGuidanceKey(code);
                    return key ? <p className="vpn-access-device-note" key={code}>{t(key)}</p> : null;
                  })}
                  {(compatibility.limitationCodes ?? []).map((code) => {
                    const key = clientCompatibilityLimitationKey(code);
                    return key ? <p className="vpn-access-device-note vpn-access-device-note-warning" key={code}>{t(key)}</p> : null;
                  })}

                  {renameMutation.isError && renamingId === null && (
                    <div className="form-message form-message-error">{getErrorMessage(renameMutation.error, t('accessDevices.renameError'))}</div>
                  )}

                  {!access.hasActiveToken ? (
                    <div className="form-message form-message-warning vpn-access-device-empty">
                      <strong>{t('accessDevices.noTokenTitle')}</strong>
                      <span>{t('accessDevices.noTokenHint')}</span>
                    </div>
                  ) : reveal ? (
                    <>
                      <div className="form-message form-message-success vpn-access-device-reveal">
                        <strong>{t('accessDevices.newAccessTitle')}</strong>
                        <span>{t('accessDevices.newAccessHint')}</span>
                      </div>
                      <code className="vpn-access-device-url">{reveal.subscriptionUrl}</code>
                    </>
                  ) : (
                    <div className="form-message form-message-warning vpn-access-device-hidden">
                      <strong>{t('accessDevices.hiddenLinkTitle')}</strong>
                      <span>{t('accessDevices.hiddenLinkHint')}</span>
                    </div>
                  )}

                  {!access.hasActiveToken ? (
                    <div className="form-actions">
                      <button
                        className="primary-button"
                        type="button"
                        disabled={isRotatingThis}
                        onClick={() => rotateMutation.mutate(device.id)}
                      >
                        {isRotatingThis ? t('accessDevices.creatingLink') : t('accessDevices.createLink')}
                      </button>
                      {!isRenaming && (
                        <button className="small-button" type="button" onClick={() => startRename(access)}>{t('accessDevices.rename')}</button>
                      )}
                      <button
                        className="small-button danger-button"
                        type="button"
                        disabled={isRevokingThis}
                        onClick={() => handleRevoke(device.id)}
                      >
                        {isRevokingThis ? t('accessDevices.revoking') : t('accessDevices.revoke')}
                      </button>
                    </div>
                  ) : (
                    <div className="form-actions">
                      <button
                        className="small-button"
                        type="button"
                        disabled={!reveal}
                        onClick={() => setQrDeviceId(device.id)}
                      >
                        {t('accessDevices.showQr')}
                      </button>
                      <button
                        className="small-button"
                        type="button"
                        disabled={!reveal}
                        onClick={() => reveal && void copyLink(device.id, reveal.subscriptionUrl)}
                      >
                        {copiedId === device.id ? t('clientCompatibility.copied') : t('accessDevices.copyLink')}
                      </button>
                      <button
                        className="small-button"
                        type="button"
                        disabled={!reveal}
                        onClick={() => toggleSend(device.id)}
                      >
                        {t('accessDevices.send')}
                      </button>
                      <button
                        className="small-button"
                        type="button"
                        disabled={isRotatingThis}
                        onClick={() => rotateMutation.mutate(device.id)}
                      >
                        {isRotatingThis ? t('accessDevices.rotating') : t('accessDevices.rotate')}
                      </button>
                      {!isRenaming && (
                        <button className="small-button" type="button" onClick={() => startRename(access)}>{t('accessDevices.rename')}</button>
                      )}
                      <button
                        className="small-button danger-button"
                        type="button"
                        disabled={isRevokingThis}
                        onClick={() => handleRevoke(device.id)}
                      >
                        {isRevokingThis ? t('accessDevices.revoking') : t('accessDevices.revoke')}
                      </button>
                    </div>
                  )}
                  {device.id in rotateErrors && (
                    <div className="form-message form-message-error">{getErrorMessage(rotateErrors[device.id], t('accessDevices.rotateError'))}</div>
                  )}
                  {device.id in revokeErrors && (
                    <div className="form-message form-message-error">{getErrorMessage(revokeErrors[device.id], t('accessDevices.revokeError'))}</div>
                  )}

                  {isSending && reveal && (
                    <DeviceSendComposer
                      accountId={accountId}
                      deviceId={device.id}
                      deviceName={device.name}
                      accessUrl={reveal.subscriptionUrl}
                      onClose={() => setSendDeviceId(null)}
                    />
                  )}
                </section>
              );
            })}
          </div>
        )}

        {revokedDevices.length > 0 && (
          <details className="vpn-access-devices-revoked">
            <summary>{t('accessDevices.revokedSummary', { count: revokedDevices.length })}</summary>
            {revokedDevices.map((access) => (
              <div className="vpn-access-device-revoked-row" key={access.device.id}>
                <span>{access.device.name}</span>
                <span>{clientTypeLabel(access.device.clientType)}</span>
                <span>{t('accessDevices.revokedAt', { date: formatDate(access.device.revokedAt) })}</span>
              </div>
            ))}
          </details>
        )}

        {isAddOpen ? (
          <form className="vpn-access-device-add-form" onSubmit={handleCreate}>
            <div className="vpn-account-create-grid">
              <label className="field">
                <span>{t('accessDevices.deviceName')}</span>
                <input value={name} onChange={(event) => setName(event.target.value)} placeholder={t('accessDevices.deviceNamePlaceholder')} maxLength={100} />
              </label>
              <label className="field">
                <span>{t('accessDevices.client')}</span>
                <select value={clientType} onChange={(event) => setClientType(event.target.value as DeviceClientType)}>
                  <option value="hiddify">Hiddify · {t('accessDevices.recommended')}</option>
                  <option value="v2rayn">v2rayN</option>
                  <option value="v2rayng">v2rayNG</option>
                  <option value="generic">{t('accessDevices.genericClient')}</option>
                </select>
              </label>
              <label className="field">
                <span>{t('accessDevices.platform')}</span>
                <select value={deviceType} onChange={(event) => setDeviceType(event.target.value as DevicePlatform)}>
                  <option value="ios">{t('accessDevices.platformIos')}</option>
                  <option value="android">{t('accessDevices.platformAndroid')}</option>
                  <option value="windows">{t('accessDevices.platformWindows')}</option>
                  <option value="macos">{t('accessDevices.platformMacos')}</option>
                  <option value="linux">{t('accessDevices.platformLinux')}</option>
                  <option value="other">{t('accessDevices.platformOther')}</option>
                </select>
              </label>
            </div>
            {createMutation.isError && (
              <div className="form-message form-message-error">{getErrorMessage(createMutation.error, t('accessDevices.createError'))}</div>
            )}
            <div className="form-actions">
              <button className="primary-button" type="submit" disabled={createMutation.isPending}>
                {createMutation.isPending ? t('accessDevices.creating') : t('accessDevices.create')}
              </button>
              <button className="small-button" type="button" onClick={() => { setIsAddOpen(false); clearAddDeviceParam(); }}>{t('common.cancel')}</button>
            </div>
          </form>
        ) : (
          <div className="form-actions">
            <button className="small-button" type="button" onClick={() => setIsAddOpen(true)}>{t('accessDevices.addDevice')}</button>
          </div>
        )}
      </div>

      <SubscriptionQrDialog
        isOpen={Boolean(qrDeviceId && qrAccess)}
        title={t('accessDevices.qrTitle')}
        onClose={() => setQrDeviceId(null)}
        qrText={qrAccess?.subscriptionUrl}
        url={qrAccess?.subscriptionUrl}
        urlLabel={t('accessDevices.copyLink')}
        onCopyQrText={() => qrDeviceId && qrAccess && void copyLink(qrDeviceId, qrAccess.subscriptionUrl)}
        copyQrLabel={t('accessDevices.copyLink')}
        copyCopiedLabel={t('clientCompatibility.copied')}
        copied={copiedId === qrDeviceId}
        closeLabel={t('clientCompatibility.close')}
      />

      <SubscriptionQrDialog
        isOpen={Boolean(legacyQrOpen && legacyRevealed)}
        title={t('accessDevices.qrTitle')}
        onClose={() => setLegacyQrOpen(false)}
        qrText={legacyRevealed?.subscriptionUrl}
        url={legacyRevealed?.subscriptionUrl}
        urlLabel={t('accessDevices.copyLink')}
        onCopyQrText={() => legacyRevealed && void copyLink('legacy', legacyRevealed.subscriptionUrl)}
        copyQrLabel={t('accessDevices.copyLink')}
        copyCopiedLabel={t('clientCompatibility.copied')}
        copied={copiedId === 'legacy'}
        closeLabel={t('clientCompatibility.close')}
      />
    </div>
  );
}
