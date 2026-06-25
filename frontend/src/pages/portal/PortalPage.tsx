import { useEffect, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import {
  getPortalDashboard,
  getPortalInstruction,
  getPortalInstructions,
  getPortalMe,
  getPortalProfile,
  getPortalProfiles,
  getPortalQRCode,
  getPortalSubscription,
  type InstructionPlatform,
  type PortalProfile,
} from '../../entities/portal/api/portalApi';
import { ScannableQrCode } from '../../shared/qr/ScannableQrCode';

function formatDate(value?: string | null): string {
  if (!value) {
    return '-';
  }

  const date = new Date(value);

  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
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

  return <span className={`badge badge-${statusClassName}`}>{normalizedStatus}</span>;
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="detail-row">
      <span>{label}</span>
      <strong>{children}</strong>
    </div>
  );
}

function PortalProfileRow({ profile, selected }: { profile: PortalProfile; selected: boolean }) {
  return (
    <Link
      className={`admin-table-row portal-profiles-table-row portal-profile-row-link${selected ? ' portal-profile-row-selected' : ''}`}
      to={`/portal/profiles/${profile.id}`}
    >
      <div>
        <strong>{formatValue(profile.displayName)}</strong>
        <span>{formatValue(profile.location)}</span>
      </div>
      <StatusBadge status={profile.accessStatus} />
      <span>{formatValue(profile.protocol)}</span>
      <span>{formatDate(profile.expiresAt)}</span>
      <span>{formatDate(profile.updatedAt)}</span>
    </Link>
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
      onClick={() => onSelect(platform.platform)}
    >
      <strong>{platform.displayName}</strong>
      <span>{platform.description}</span>
    </button>
  );
}

export function PortalPage() {
  const { profileId } = useParams<{ profileId: string }>();
  const [selectedPlatform, setSelectedPlatform] = useState<string | null>(null);
  const [copiedTarget, setCopiedTarget] = useState<string | null>(null);

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

  const profileQuery = useQuery({
    queryKey: ['portal-profile', profileId],
    queryFn: () => getPortalProfile(profileId ?? ''),
    enabled: Boolean(profileId),
    retry: false,
  });

  const profiles = profilesQuery.data?.items ?? [];
  const selectedProfile = profileQuery.data?.profile ?? profiles.find((profile) => profile.id === profileId);

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

  const copyToClipboard = async (target: string, value?: string) => {
    if (!value || !navigator.clipboard) {
      return;
    }

    await navigator.clipboard.writeText(value);
    setCopiedTarget(target);
    window.setTimeout(() => setCopiedTarget(null), 1800);
  };

  const hasPortalLoadError = meQuery.isError || dashboardQuery.isError || profilesQuery.isError;

  return (
    <section className="page portal-page">
      <div className="page-header">
        <div>
          <h1>User Portal</h1>
          <p>Self-service VPN profile overview and setup surface.</p>
        </div>

        <div className="status-pill">
          <span className={hasPortalLoadError ? 'status-dot status-dot-warn' : 'status-dot status-dot-ok'} />
          {hasPortalLoadError ? 'Portal access check failed' : 'Portal session'}
        </div>
      </div>

      {hasPortalLoadError && (
        <div className="form-message form-message-error portal-message">
          Unable to load the portal. Sign in with an account that has portal access.
        </div>
      )}

      <div className="portal-card-grid">
        <div className="card portal-card">
          <div className="card-title">Current user</div>
          <div className="card-value card-value-small">
            {meQuery.isLoading ? '...' : formatValue(portalUser?.displayName || portalUser?.username)}
          </div>
          <div className="card-meta">{formatValue(portalUser?.email)}</div>
        </div>

        <div className="card portal-card">
          <div className="card-title">Access status</div>
          <div className="card-value card-value-small">
            {dashboardQuery.isLoading ? '...' : <StatusBadge status={dashboard?.accessStatus} />}
          </div>
          <div className="card-meta">Overall portal access state.</div>
        </div>

        <div className="card portal-card">
          <div className="card-title">VPN profiles</div>
          <div className="card-value">{profilesQuery.isLoading ? '...' : dashboard?.profilesTotal ?? profiles.length}</div>
          <div className="card-meta">{dashboard?.profilesActive ?? 0} active profile(s).</div>
        </div>

        <div className="card portal-card">
          <div className="card-title">Nearest expiration</div>
          <div className="card-value card-value-small">{formatDate(dashboard?.nearestExpiration)}</div>
          <div className="card-meta">Traffic usage is reserved for a later milestone.</div>
        </div>
      </div>

      {dashboard && dashboard.notices.length > 0 && (
        <div className="portal-notices">
          {dashboard.notices.map((notice, index) => (
            <div className="form-message" key={`${notice.type}-${index}`}>
              {notice.message}
            </div>
          ))}
        </div>
      )}

      <div className="portal-layout">
        <div className="panel admin-table-panel">
          <div className="panel-title">VPN profiles</div>

          {profilesQuery.isLoading && <p className="empty-state">Loading VPN profiles...</p>}

          {profilesQuery.isError && (
            <div className="form-message form-message-error">Failed to load VPN profiles.</div>
          )}

          {profilesQuery.isSuccess && profiles.length === 0 && (
            <p className="empty-state">No VPN profiles are available for this user yet.</p>
          )}

          {profiles.length > 0 && (
            <div className="admin-table portal-profiles-table">
              <div className="admin-table-row admin-table-head portal-profiles-table-row">
                <span>Profile</span>
                <span>Access</span>
                <span>Protocol</span>
                <span>Expires</span>
                <span>Updated</span>
              </div>
              {profiles.map((profile) => (
                <PortalProfileRow
                  key={profile.id}
                  profile={profile}
                  selected={profile.id === profileId}
                />
              ))}
            </div>
          )}
        </div>

        <div className="portal-profile-panels">
          <div className="panel credentials-panel">
            <div className="panel-header">
              <div>
                <div className="panel-title">Profile details</div>
                <p className="panel-subtitle">User-facing VPN profile metadata.</p>
              </div>
            </div>

            {!profileId && <p className="empty-state">Select a VPN profile to view details.</p>}

            {profileId && profileQuery.isLoading && (
              <p className="empty-state">Loading profile details...</p>
            )}

            {profileId && profileQuery.isError && (
              <div className="form-message form-message-error">Failed to load this VPN profile.</div>
            )}

            {selectedProfile && (
              <div className="detail-list credentials-detail-list">
                <DetailRow label="Profile ID">
                  <code>{formatValue(selectedProfile.id)}</code>
                </DetailRow>
                <DetailRow label="Display name">{formatValue(selectedProfile.displayName)}</DetailRow>
                <DetailRow label="Status"><StatusBadge status={selectedProfile.status} /></DetailRow>
                <DetailRow label="Access"><StatusBadge status={selectedProfile.accessStatus} /></DetailRow>
                <DetailRow label="Protocol">{formatValue(selectedProfile.protocol)}</DetailRow>
                <DetailRow label="Location">{formatValue(selectedProfile.location)}</DetailRow>
                <DetailRow label="Max devices">{formatValue(selectedProfile.maxDevices)}</DetailRow>
                <DetailRow label="Expires at">{formatDate(selectedProfile.expiresAt)}</DetailRow>
                <DetailRow label="Updated at">{formatDate(selectedProfile.updatedAt)}</DetailRow>
              </div>
            )}
          </div>

          {selectedProfile && (
            <div className="portal-profile-grid">
              <div className="panel subscription-panel">
                <div className="panel-header">
                  <div>
                    <div className="panel-title">Subscription metadata</div>
                    <p className="panel-subtitle">
                      The portal only displays backend-provided subscription data.
                    </p>
                  </div>
                </div>

                {subscriptionQuery.isLoading && <p className="empty-state">Loading subscription metadata...</p>}

                {subscriptionQuery.isError && (
                  <div className="form-message form-message-error">Failed to load subscription metadata.</div>
                )}

                {subscription && (
                  <div className="subscription-result">
                    <div className="detail-list credentials-detail-list">
                      <DetailRow label="Profile ID">
                        <code>{formatValue(subscription.profileId)}</code>
                      </DetailRow>
                      <DetailRow label="Available">{subscription.available ? 'yes' : 'no'}</DetailRow>
                      <DetailRow label="Format">{formatValue(subscription.format)}</DetailRow>
                      <DetailRow label="Expires at">{formatDate(subscription.expiresAt)}</DetailRow>
                      <DetailRow label="Token rotation required">
                        {subscription.requiresTokenRotation ? 'yes' : 'no'}
                      </DetailRow>
                      {subscription.subscriptionUrl && (
                        <DetailRow label="Subscription URL">
                          <span className="copyable-value">
                            <code>{subscription.subscriptionUrl}</code>
                            <button
                              className="small-button"
                              type="button"
                              onClick={() => void copyToClipboard('portal-subscription-url', subscription.subscriptionUrl)}
                            >
                              {copiedTarget === 'portal-subscription-url' ? 'Copied' : 'Copy'}
                            </button>
                          </span>
                        </DetailRow>
                      )}
                    </div>

                    {subscription.message && (
                      <div className="form-message form-message-warning">{subscription.message}</div>
                    )}
                  </div>
                )}
              </div>

              <div className="panel subscription-panel">
                <div className="panel-header">
                  <div>
                    <div className="panel-title">QR metadata</div>
                    <p className="panel-subtitle">Rendered only when the backend returns QR text.</p>
                  </div>
                </div>

                {qrQuery.isLoading && <p className="empty-state">Loading QR metadata...</p>}

                {qrQuery.isError && (
                  <div className="form-message form-message-error">Failed to load QR metadata.</div>
                )}

                {qr && (
                  <div className="qr-payload-panel portal-qr-panel">
                    {qr.available && qr.qrText ? (
                      <ScannableQrCode
                        value={qr.qrText}
                        title="Subscription QR code"
                        subtitle={`Format: ${formatValue(qr.format)}`}
                      />
                    ) : (
                      <p className="empty-state">{qr.message ?? 'QR payload is not available yet.'}</p>
                    )}

                    <div className="detail-list credentials-detail-list">
                      <DetailRow label="Profile ID">
                        <code>{formatValue(qr.profileId)}</code>
                      </DetailRow>
                      <DetailRow label="Available">{qr.available ? 'yes' : 'no'}</DetailRow>
                      <DetailRow label="Format">{formatValue(qr.format)}</DetailRow>
                    </div>

                    {qr.available && qr.qrText && (
                      <>
                        <pre className="code-block">{qr.qrText}</pre>
                        <button
                          className="small-button"
                          type="button"
                          onClick={() => void copyToClipboard('portal-qr-text', qr.qrText)}
                        >
                          {copiedTarget === 'portal-qr-text' ? 'Copied' : 'Copy QR text'}
                        </button>
                      </>
                    )}
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      </div>

      <div className="panel portal-instructions-panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">Setup instructions</div>
            <p className="panel-subtitle">Device setup guidance from the portal backend.</p>
          </div>
        </div>

        {instructionsQuery.isLoading && <p className="empty-state">Loading setup instructions...</p>}

        {instructionsQuery.isError && (
          <div className="form-message form-message-error">Failed to load setup instructions.</div>
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
              {instructionQuery.isLoading && <p className="empty-state">Loading selected instruction...</p>}

              {instructionQuery.isError && (
                <div className="form-message form-message-error">Failed to load selected instruction.</div>
              )}

              {instructionQuery.data?.instruction && (
                <div className="portal-instruction-card">
                  <div>
                    <div className="panel-title token-snippet-title">
                      {instructionQuery.data.instruction.displayName}
                    </div>
                    <p className="panel-subtitle">Platform: {instructionQuery.data.instruction.platform}</p>
                  </div>

                  <div>
                    <div className="portal-section-title">Steps</div>
                    <ol className="portal-step-list">
                      {instructionQuery.data.instruction.steps.map((step, index) => (
                        <li key={`${step}-${index}`}>{step}</li>
                      ))}
                    </ol>
                  </div>

                  {instructionQuery.data.instruction.notes.length > 0 && (
                    <div>
                      <div className="portal-section-title">Notes</div>
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
    </section>
  );
}
