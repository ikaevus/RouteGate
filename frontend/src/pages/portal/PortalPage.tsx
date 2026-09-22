import { useEffect, useState, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import {
  generatePortalSubscriptionAccess,
  getPortalDashboard,
  getPortalInstruction,
  getPortalInstructions,
  getPortalMe,
  getPortalProfile,
  getPortalProfiles,
  getPortalQRCode,
  getPortalSubscription,
  type InstructionPlatform,
} from '../../entities/portal/api/portalApi';
import { getCurrentLocale, t, translateStatus } from '../../shared/i18n/i18n';
import { SubscriptionQrDialog } from '../../shared/ui/SubscriptionQrDialog';
import './PortalPageV2.css';

function formatDate(value?: string | null): string {
  if (!value) {
    return t('common.notAvailable');
  }

  const date = new Date(value);

  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatShortDate(value?: string | null): string {
  if (!value) {
    return t('portalV2.noExpiration');
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat(getCurrentLocale() === 'ru' ? 'ru-RU' : 'en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  }).format(date);
}

function formatBytes(value?: number | null): string {
  if (value == null || !Number.isFinite(value) || value <= 0) {
    return '0 B';
  }

  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const unitIndex = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  const scaled = value / 1024 ** unitIndex;
  const locale = getCurrentLocale() === 'ru' ? 'ru-RU' : 'en-US';

  return `${new Intl.NumberFormat(locale, {
    maximumFractionDigits: scaled >= 10 || unitIndex === 0 ? 0 : 1,
  }).format(scaled)} ${units[unitIndex]}`;
}

function formatValue(value?: string | number | null): string {
  if (typeof value === 'number') {
    return String(value);
  }

  return value && value.trim() !== '' ? value : '-';
}

function StatusBadge({ status }: { status?: string | null }) {
  const normalizedStatus = status && status.trim() !== '' ? status : 'unknown';
  const statusClassName = normalizedStatus.toLowerCase().replace(/[^a-z0-9-]/g, '-');

  return (
    <span className={`badge badge-${statusClassName}`} style={{ whiteSpace: 'nowrap' }}>
      {translateStatus(normalizedStatus)}
    </span>
  );
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="detail-row">
      <span>{label}</span>
      <strong>{children}</strong>
    </div>
  );
}

function InstructionButton({
  platform,
  selected,
  onSelect,
}: {
  platform: InstructionPlatform;
  selected: boolean;
  onSelect: (platform: string) => void;
}) {
  return (
    <button
      className={`portal-instruction-button${selected ? ' portal-instruction-button-selected' : ''}`}
      type="button"
      aria-pressed={selected}
      onClick={() => onSelect(platform.platform)}
    >
      <strong>{platform.displayName}</strong>
      <span>{platform.description}</span>
    </button>
  );
}

export function PortalPage() {
  const { profileId } = useParams<{ profileId: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [selectedPlatform, setSelectedPlatform] = useState<string | null>(null);
  const [copiedTarget, setCopiedTarget] = useState<string | null>(null);
  const [isQrOpen, setIsQrOpen] = useState(false);

  const meQuery = useQuery({
    queryKey: ['portal-me'],
    queryFn: getPortalMe,
    retry: false,
  });

  const dashboardQuery = useQuery({
    queryKey: ['portal-dashboard'],
    queryFn: getPortalDashboard,
    retry: false,
  });

  const profilesQuery = useQuery({
    queryKey: ['portal-profiles'],
    queryFn: getPortalProfiles,
    retry: false,
  });

  const profiles = profilesQuery.data?.items ?? [];
  const effectiveProfileId = profileId ?? (profiles.length === 1 ? profiles[0].id : undefined);
  const profileQuery = useQuery({
    queryKey: ['portal-profile', effectiveProfileId],
    queryFn: () => getPortalProfile(effectiveProfileId ?? ''),
    enabled: Boolean(effectiveProfileId),
    retry: false,
  });
  const selectedProfile = profileQuery.data?.profile;

  useEffect(() => {
    setIsQrOpen(false);
    setCopiedTarget(null);
  }, [effectiveProfileId]);

  const subscriptionQuery = useQuery({
    queryKey: ['portal-profile-subscription', selectedProfile?.id],
    queryFn: () => getPortalSubscription(selectedProfile?.id ?? ''),
    enabled: Boolean(selectedProfile?.id),
    retry: false,
  });

  const qrQuery = useQuery({
    queryKey: ['portal-profile-qr', selectedProfile?.id],
    queryFn: () => getPortalQRCode(selectedProfile?.id ?? ''),
    enabled: Boolean(selectedProfile?.id),
    retry: false,
  });

  const subscriptionAccessMutation = useMutation({
    mutationFn: (id: string) => generatePortalSubscriptionAccess(id),
    onSuccess: (data) => {
      queryClient.setQueryData(['portal-profile-subscription', data.subscription.profileId], {
        subscription: data.subscription,
      });
      queryClient.setQueryData(['portal-profile-qr', data.qr.profileId], {
        qr: data.qr,
      });
    },
  });

  const instructionsQuery = useQuery({
    queryKey: ['portal-instructions'],
    queryFn: getPortalInstructions,
    retry: false,
  });

  const instructionPlatforms = instructionsQuery.data?.items ?? [];

  useEffect(() => {
    if (!selectedPlatform && instructionPlatforms.length > 0) {
      setSelectedPlatform(instructionPlatforms[0].platform);
    }
  }, [instructionPlatforms, selectedPlatform]);

  const instructionQuery = useQuery({
    queryKey: ['portal-instruction', selectedPlatform],
    queryFn: () => getPortalInstruction(selectedPlatform ?? ''),
    enabled: Boolean(selectedPlatform),
    retry: false,
  });

  const dashboard = dashboardQuery.data?.dashboard;
  const portalUser = meQuery.data?.user;
  const subscription = subscriptionQuery.data?.subscription;
  const qr = qrQuery.data?.qr;
  const canGenerateSubscription = selectedProfile?.accessStatus === 'active';
  const userDisplayName = portalUser?.displayName || portalUser?.username || portalUser?.email || t('common.unknown');
  const trafficUsage = dashboard?.trafficUsage;

  const copyToClipboard = async (target: string, value?: string) => {
    if (!value) return;
    if (!navigator.clipboard) {
      setCopiedTarget('error');
      return;
    }

    try {
      await navigator.clipboard.writeText(value);
    } catch {
      setCopiedTarget('error');
      return;
    }
    setCopiedTarget(target);
    window.setTimeout(() => setCopiedTarget(null), 1800);
  };

  const hasPortalLoadError = meQuery.isError || dashboardQuery.isError || profilesQuery.isError;

  return (
    <section className="page portal-page portal-connect-page" style={{ overflowX: 'hidden' }}>
      {hasPortalLoadError && (
        <div className="form-message form-message-error portal-message">
          {t('portal.loadError')}
        </div>
      )}

      <header className="portal-connect-heading">
        <div className="portal-v2-eyebrow">{t('portalV2.eyebrow')}</div>
        <h1>{t('portalV2.connectDevice')}</h1>
        <p>{t('portalV2.connectionIntro')}</p>
      </header>

      {dashboard && dashboard.notices.length > 0 && (
        <div className="portal-notices">
          {dashboard.notices.map((notice, index) => (
            <div className="form-message" key={`${notice.type}-${index}`}>
              {notice.message}
            </div>
          ))}
        </div>
      )}

      <div className="panel portal-connect-profile">
        {profilesQuery.isLoading && <p role="status">{t('portal.loadingProfiles')}</p>}
        {profilesQuery.isSuccess && profiles.length === 0 && <p>{t('portal.noProfiles')}</p>}
        {profiles.length > 1 && (
          <label className="portal-profile-picker">
            <span>{t('portalV2.chooseProfile')}</span>
            <select value={profileId ?? ''} onChange={(event) => navigate(event.target.value ? `/portal/profiles/${event.target.value}` : '/portal')}>
              <option value="">{t('portal.selectProfile')}</option>
              {profiles.map((profile) => (
                <option key={profile.id} value={profile.id}>
                  {profile.displayName} · {profile.location} · {translateStatus(profile.accessStatus)}
                </option>
              ))}
            </select>
          </label>
        )}
        {effectiveProfileId && profileQuery.isLoading && <p role="status">{t('portal.loadingProfileDetails')}</p>}
        {effectiveProfileId && profileQuery.isError && <div className="form-message form-message-error">{t('portal.profileLoadError')}</div>}
        {selectedProfile && (
          <div className="portal-connect-summary">
            <strong>{selectedProfile.displayName}</strong>
            <span>{selectedProfile.location} · {selectedProfile.protocol}</span>
            <StatusBadge status={selectedProfile.accessStatus} />
          </div>
        )}
        {selectedProfile && !canGenerateSubscription && <p className="form-message form-message-warning">{t('portalV2.noActiveProfile')}</p>}
      </div>

      {selectedProfile && (
        <div
          className="portal-connect-access"
          key={selectedProfile.id}
        >
          <div className="panel subscription-panel">
            <div className="panel-header">
              <div>
                <div className="panel-title">{t('portal.subscriptionTitle')}</div>
                <p className="panel-subtitle">{t('portal.subscriptionSubtitle')}</p>
              </div>
              {(!subscription?.available || subscription.requiresTokenRotation) && <button
                className="portal-v2-primary-action"
                type="button"
                disabled={!canGenerateSubscription || subscriptionQuery.isLoading || subscriptionAccessMutation.isPending}
                onClick={() => subscriptionAccessMutation.mutate(selectedProfile.id)}
              >
                {subscriptionAccessMutation.isPending ? t('portal.generating') : t('portalV2.createConnection')}
              </button>}
            </div>

            {subscriptionQuery.isLoading && <p className="empty-state">{t('portal.loadingSubscription')}</p>}

            {subscriptionQuery.isError && (
              <div className="form-message form-message-error">{t('portal.subscriptionLoadError')}</div>
            )}

            {copiedTarget === 'error' && <p role="alert">{t('portalV2.copyError')}</p>}

            {subscriptionAccessMutation.variables === selectedProfile.id && subscriptionAccessMutation.isError && (
              <div className="form-message form-message-error">
                {t('portal.subscriptionGenerateError')}
              </div>
            )}

            {subscriptionAccessMutation.variables === selectedProfile.id && subscriptionAccessMutation.isSuccess && (
              <div className="form-message">
                {t('portal.subscriptionGenerated')}
              </div>
            )}

            {subscription && (
              <div className="subscription-result subscription-self-service-card">
                {subscription.available && canGenerateSubscription && subscription.subscriptionUrl && <div className="subscription-url-stack">
                  <div className="subscription-url-header">
                    <div className="subscription-url-meta">
                      <div className="subscription-url-label">{t('portal.subscriptionUrl')}</div>
                      <p className="subscription-url-helper">{t('portal.subscriptionUrlHelper')}</p>
                    </div>
                    <div className="table-actions">
                      <button
                        className="small-button"
                        type="button"
                        onClick={() => void copyToClipboard('portal-subscription-url', subscription.subscriptionUrl)}
                      >
                        {copiedTarget === 'portal-subscription-url' ? t('portal.copied') : t('portal.copy')}
                      </button>
                      <button
                        className="small-button"
                        type="button"
                        onClick={() => setIsQrOpen(true)}
                        disabled={!qr?.available || !qr.qrText}
                      >
                        {t('portal.showQr')}
                      </button>
                    </div>
                  </div>
                  <code className="subscription-url-value">{subscription.subscriptionUrl}</code>
                </div>}

                {subscription.message && (
                  <div className="form-message form-message-warning">{subscription.message}</div>
                )}
              </div>
            )}
          </div>

          <SubscriptionQrDialog
            isOpen={isQrOpen && canGenerateSubscription && Boolean(qr?.available)}
            title={t('portal.subscriptionQrCode')}
            onClose={() => setIsQrOpen(false)}
            qrText={qr?.qrText}
            qrTitle={t('portal.subscriptionQrCode')}
            qrSubtitle={t('portal.formatValue', { format: formatValue(qr?.format) })}
            url={subscription?.subscriptionUrl}
            urlLabel={t('portal.subscriptionUrl')}
            onCopyQrText={() => void copyToClipboard('portal-qr-text', qr?.qrText ?? '')}
            copyQrLabel={t('portal.copyQrText')}
            copyCopiedLabel={t('portal.copied')}
            copied={copiedTarget === 'portal-qr-text'}
            closeLabel={t('common.close')}
            loadingLabel={t('portal.qrUnavailable')}
            unavailableLabel={t('portal.qrUnavailable')}
          />
        </div>
      )}

      <div className="panel portal-instructions-panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">{t('portal.setupInstructions')}</div>
            <p className="panel-subtitle">{t('portal.setupInstructionsSubtitle')}</p>
          </div>
        </div>

        {instructionsQuery.isLoading && <p className="empty-state">{t('portal.loadingSetupInstructions')}</p>}

        {instructionsQuery.isError && (
          <div className="form-message form-message-error">{t('portal.setupInstructionsLoadError')}</div>
        )}

        {instructionPlatforms.length > 0 && (
          <div className="portal-instructions-layout">
            <div className="portal-instruction-buttons">
              {instructionPlatforms.map((platform) => (
                <InstructionButton
                  key={platform.platform}
                  platform={platform}
                  selected={platform.platform === selectedPlatform}
                  onSelect={setSelectedPlatform}
                />
              ))}
            </div>

            <div className="portal-instruction-detail">
              {instructionQuery.isLoading && <p className="empty-state">{t('portal.loadingSelectedInstruction')}</p>}

              {instructionQuery.isError && (
                <div className="form-message form-message-error">{t('portal.selectedInstructionLoadError')}</div>
              )}

              {instructionQuery.data?.instruction && (
                <div className="portal-instruction-card">
                  <div>
                    <div className="panel-title token-snippet-title">
                      {instructionQuery.data.instruction.displayName}
                    </div>
                    <p className="panel-subtitle">{t('portal.platform', { platform: instructionQuery.data.instruction.platform })}</p>
                  </div>

                  <div>
                    <div className="portal-section-title">{t('portal.steps')}</div>
                    <ol className="portal-step-list">
                      {instructionQuery.data.instruction.steps.map((step, index) => (
                        <li key={`${step}-${index}`}>{step}</li>
                      ))}
                    </ol>
                  </div>

                  {instructionQuery.data.instruction.notes.length > 0 && (
                    <div>
                      <div className="portal-section-title">{t('portal.notes')}</div>
                      <ul className="portal-note-list">
                        {instructionQuery.data.instruction.notes.map((note, index) => (
                          <li key={`${note}-${index}`}>{note}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
        )}
      </div>
      <details className="panel portal-connect-details">
        <summary>{t('portalV2.usageDetails')}</summary>
        <p>{t('portalV2.greeting', { name: userDisplayName })}</p>
        <div className="portal-v2-metrics">
          <div className="portal-v2-metric">
            <span>{t('portalV2.access')}</span>
            <strong>{dashboardQuery.isLoading ? '...' : <StatusBadge status={dashboard?.accessStatus} />}</strong>
            <small>{t('portal.accessStatusMeta')}</small>
          </div>

          <div className="portal-v2-metric">
            <span>{t('portalV2.profiles')}</span>
            <strong>{profilesQuery.isLoading ? '...' : dashboard?.profilesTotal ?? profiles.length}</strong>
            <small>
              {t('portalV2.profilesValue', {
                active: dashboard?.profilesActive ?? 0,
                total: dashboard?.profilesTotal ?? profiles.length,
              })}
            </small>
          </div>

          <div className="portal-v2-metric">
            <span>{t('portalV2.trafficThisMonth')}</span>
            <strong>{dashboardQuery.isLoading ? '...' : trafficUsage?.enabled ? formatBytes(trafficUsage.totalBytes) : t('common.notAvailable')}</strong>
            <small className="portal-v2-traffic-split">
              {trafficUsage?.enabled
                ? t('portalV2.rxTx', {
                    rx: formatBytes(trafficUsage.rxBytes),
                    tx: formatBytes(trafficUsage.txBytes),
                  })
                : t('portalV2.noTrafficYet')}
            </small>
          </div>

          <div className="portal-v2-metric">
            <span>{t('portalV2.expires')}</span>
            <strong>{dashboardQuery.isLoading ? '...' : formatShortDate(dashboard?.nearestExpiration)}</strong>
            <small>
              {trafficUsage?.lastObservedAt
                ? t('portalV2.lastTraffic', { date: formatDate(trafficUsage.lastObservedAt) })
                : t('portalV2.noTrafficYet')}
            </small>
          </div>
        </div>
            {selectedProfile && (
              <div className="detail-list credentials-detail-list">
                {subscription?.available && !subscription.requiresTokenRotation && (
                  <button className="small-button" type="button"
                    disabled={!canGenerateSubscription || subscriptionAccessMutation.isPending}
                    onClick={() => subscriptionAccessMutation.mutate(selectedProfile.id)}>
                    {subscriptionAccessMutation.isPending ? t('portal.generating') : t('portal.generateRefresh')}
                  </button>
                )}
                <DetailRow label={t('portal.profileId')}>
                  <code>{formatValue(selectedProfile.id)}</code>
                </DetailRow>
                <DetailRow label={t('portal.displayName')}>{formatValue(selectedProfile.displayName)}</DetailRow>
                <DetailRow label={t('portal.status')}><StatusBadge status={selectedProfile.status} /></DetailRow>
                <DetailRow label={t('portal.access')}><StatusBadge status={selectedProfile.accessStatus} /></DetailRow>
                <DetailRow label={t('portal.protocol')}>{formatValue(selectedProfile.protocol)}</DetailRow>
                <DetailRow label={t('portal.location')}>{formatValue(selectedProfile.location)}</DetailRow>
                <DetailRow label={t('portal.maxDevices')}>{formatValue(selectedProfile.maxDevices)}</DetailRow>
                <DetailRow label={t('portal.expiresAt')}>{formatDate(selectedProfile.expiresAt)}</DetailRow>
                <DetailRow label={t('portal.updatedAt')}>{formatDate(selectedProfile.updatedAt)}</DetailRow>
              </div>
            )}
      </details>
    </section>
  );
}
