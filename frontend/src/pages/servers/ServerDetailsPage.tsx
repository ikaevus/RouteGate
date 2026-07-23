import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import { ApiError } from '../../shared/api/client';
import {
  applyConfigVersion,
  assignServerRoutingProfile,
  clearServerRoutingProfile,
  createServerRegistrationToken,
  deleteServer,
  getConfigApplyJobs,
  getConfigVersions,
  getServer,
  getServerRoutingProfile,
  renderConfig,
  updateServer,
  validateConfigVersion,
  type ConfigApplyJob,
  type ConfigVersion,
  type RegistrationTokenResponse,
} from '../../entities/server/api/serverApi';
import { getRoutingProfiles } from '../../entities/routingProfile/api/routingProfileApi';
import { t, translateStatus } from '../../shared/i18n/i18n';

function formatDate(value?: string | null): string {
  if (!value) {
    return t('common.notAvailable');
  }

  const date = new Date(value);

  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatValue(value?: string | null): string {
  return value && value.trim() !== '' ? value : '—';
}

function formatCapabilities(capabilities?: Record<string, unknown> | null): string {
  if (!capabilities || Object.keys(capabilities).length === 0) {
    return '—';
  }

  return JSON.stringify(capabilities, null, 2);
}

function isValidIpAddress(value: string): boolean {
  const candidate = value.trim();
  const ipv4Parts = candidate.split('.');

  if (
    ipv4Parts.length === 4
    && ipv4Parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255)
  ) {
    return true;
  }

  if (!candidate.includes(':')) {
    return false;
  }

  try {
    const parsed = new URL(`http://[${candidate}]/`);
    return parsed.hostname.length > 2;
  } catch {
    return false;
  }
}

function getUpdateErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === 'name_required') {
      return t('serverDetails.editValidationNameRequired');
    }
    if (error.status === 400) {
      return t('serverDetails.editValidationPublicIpInvalid');
    }
    if (error.status === 403) {
      return t('serverDetails.editPermissionError');
    }
  }

  return t('serverDetails.editError');
}

function getDeleteErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === 'server_in_use' || error.status === 409) {
      return t('serverDetails.deleteInUseError');
    }
    if (error.status === 403) {
      return t('serverDetails.deletePermissionError');
    }
  }

  return t('serverDetails.deleteError');
}

function StatusBadge({ status }: { status?: string | null }) {
  const normalizedStatus = status && status.trim() !== '' ? status : 'unknown';
  const statusClassName = normalizedStatus.toLowerCase().replace(/[^a-z0-9-]/g, '-');

  return <span className={`badge badge-${statusClassName}`}>{translateStatus(normalizedStatus)}</span>;
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="detail-row">
      <span>{label}</span>
      <strong>{children}</strong>
    </div>
  );
}

function RegistrationTokenResult({
  registrationToken,
  configSnippet,
  onCopy,
  isCopied = false,
  isConfigCollapsible = false,
}: {
  registrationToken: RegistrationTokenResponse;
  configSnippet: string;
  onCopy?: () => void;
  isCopied?: boolean;
  isConfigCollapsible?: boolean;
}) {
  const [isConfigExpanded, setIsConfigExpanded] = useState(false);

  return (
    <div className="token-result">
      <div className="form-message form-message-warning" role="status">
        {t('serverDetails.registrationTokenWarning')}
      </div>
      <div className="registration-token-display">
        <span>{t('serverDetails.registrationToken')}</span>
        <div className="registration-token-field">
          <code>{registrationToken.registrationToken}</code>
          {onCopy && (
            <button
              className="small-button registration-token-copy-button"
              type="button"
              autoFocus
              onClick={onCopy}
            >
              {t('serverDetails.copyRegistrationToken')}
            </button>
          )}
        </div>
        {isCopied && (
          <span className="registration-token-copy-feedback" role="status">
            {t('serverDetails.registrationTokenCopied')}
          </span>
        )}
      </div>
      <DetailRow label={t('vpnAccounts.expiresAt')}>{formatDate(registrationToken.expiresAt)}</DetailRow>
      <div className="registration-token-config-example">
        {isConfigCollapsible ? (
          <button
            className="small-button registration-token-config-toggle"
            type="button"
            aria-expanded={isConfigExpanded}
            onClick={() => setIsConfigExpanded((current) => !current)}
          >
            {isConfigExpanded
              ? t('serverDetails.hideAgentConfigExample')
              : t('serverDetails.showAgentConfigExample')}
          </button>
        ) : (
          <div className="panel-title token-snippet-title">{t('serverDetails.exampleAgentConfig')}</div>
        )}
        {(!isConfigCollapsible || isConfigExpanded) && (
          <pre className="code-block">{configSnippet}</pre>
        )}
      </div>
    </div>
  );
}

function shortHash(value?: string | null): string {
  return value && value.length > 12 ? value.slice(0, 12) : formatValue(value);
}

function formatStageValue(value: unknown): string {
  if (typeof value === 'string' && value.trim() !== '') {
    return value;
  }
  if (typeof value === 'boolean') {
    return value ? 'true' : 'false';
  }
  if (typeof value === 'number') {
    return String(value);
  }
  if (value && typeof value === 'object') {
    return 'reported';
  }
  return '—';
}

function StageSummary({ resultPayload }: { resultPayload?: Record<string, unknown> | null }) {
  const stages = ['stage', 'validate', 'apply', 'restart', 'healthcheck', 'rollback'];

  return (
    <div className="stage-summary">
      {stages.map((stage) => (
        <span className="stage-pill" key={stage}>
          <span>{stage}</span>
          <strong>{formatStageValue(resultPayload?.[stage])}</strong>
        </span>
      ))}
    </div>
  );
}

export function ServerDetailsPage() {
  const { serverId } = useParams<{ serverId: string }>();
  const routeLocation = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const wasJustCreated = Boolean(
    (routeLocation.state as { serverCreated?: boolean } | null)?.serverCreated,
  );
  const openRegistrationTokenDialogOnSuccess = useRef(false);
  const [registrationToken, setRegistrationToken] = useState<RegistrationTokenResponse | null>(null);
  const [isRegistrationTokenDialogOpen, setIsRegistrationTokenDialogOpen] = useState(false);
  const [isRegistrationTokenCopied, setIsRegistrationTokenCopied] = useState(false);
  const [selectedRoutingProfileId, setSelectedRoutingProfileId] = useState('');
  const [isDeleteOpen, setIsDeleteOpen] = useState(false);
  const [deleteConfirmation, setDeleteConfirmation] = useState('');
  const [isEditing, setIsEditing] = useState(false);
  const [hasSubmittedEdit, setHasSubmittedEdit] = useState(false);
  const [updateSucceeded, setUpdateSucceeded] = useState(false);
  const [editName, setEditName] = useState('');
  const [editProvider, setEditProvider] = useState('');
  const [editLocation, setEditLocation] = useState('');
  const [editPublicIp, setEditPublicIp] = useState('');
  const [editDescription, setEditDescription] = useState('');

  const serverQuery = useQuery({
    queryKey: ['server', serverId],
    queryFn: () => getServer(serverId ?? ''),
    enabled: Boolean(serverId),
  });

  const routingProfilesQuery = useQuery({
    queryKey: ['routing-profiles'],
    queryFn: getRoutingProfiles,
  });

  const serverRoutingProfileQuery = useQuery({
    queryKey: ['server-routing-profile', serverId],
    queryFn: () => getServerRoutingProfile(serverId ?? ''),
    enabled: Boolean(serverId),
  });

  useEffect(() => {
    setSelectedRoutingProfileId(serverRoutingProfileQuery.data?.routingProfile?.id ?? '');
  }, [serverRoutingProfileQuery.data?.routingProfile?.id]);

  const registrationTokenMutation = useMutation({
    mutationFn: () => createServerRegistrationToken(serverId ?? ''),
    onMutate: () => {
      setRegistrationToken(null);
      setIsRegistrationTokenDialogOpen(false);
      setIsRegistrationTokenCopied(false);
    },
    onSuccess: (response) => {
      setRegistrationToken(response);
      if (openRegistrationTokenDialogOnSuccess.current) {
        setIsRegistrationTokenDialogOpen(true);
      }
    },
  });

  const createRegistrationToken = (showDialog: boolean) => {
    openRegistrationTokenDialogOnSuccess.current = showDialog;
    registrationTokenMutation.mutate();
  };

  const copyRegistrationToken = async () => {
    if (!registrationToken || !navigator.clipboard) {
      return;
    }

    await navigator.clipboard.writeText(registrationToken.registrationToken);
    setIsRegistrationTokenCopied(true);
    window.setTimeout(() => setIsRegistrationTokenCopied(false), 1800);
  };

  useEffect(() => {
    if (!isRegistrationTokenDialogOpen) {
      return undefined;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setIsRegistrationTokenDialogOpen(false);
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    document.body.style.overflow = 'hidden';

    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = '';
    };
  }, [isRegistrationTokenDialogOpen]);

  const configVersionsQuery = useQuery({
    queryKey: ['server-config-versions', serverId],
    queryFn: () => getConfigVersions(serverId ?? ''),
    enabled: Boolean(serverId),
  });

  const applyJobsQuery = useQuery({
    queryKey: ['server-config-apply-jobs', serverId],
    queryFn: () => getConfigApplyJobs(serverId ?? ''),
    enabled: Boolean(serverId),
  });

  const refreshConfigQueries = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['server-config-versions', serverId] }),
      queryClient.invalidateQueries({ queryKey: ['server-config-apply-jobs', serverId] }),
    ]);
  };

  const refreshRoutingProfileAssignment = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['server-routing-profile', serverId] }),
      queryClient.invalidateQueries({ queryKey: ['server-config-versions', serverId] }),
    ]);
  };

  const assignRoutingProfileMutation = useMutation({
    mutationFn: () => assignServerRoutingProfile(serverId ?? '', { routingProfileId: selectedRoutingProfileId }),
    onSuccess: refreshRoutingProfileAssignment,
  });

  const clearRoutingProfileMutation = useMutation({
    mutationFn: () => clearServerRoutingProfile(serverId ?? ''),
    onSuccess: async () => {
      setSelectedRoutingProfileId('');
      await refreshRoutingProfileAssignment();
    },
  });

  const renderConfigMutation = useMutation({
    mutationFn: () => renderConfig(serverId ?? ''),
    onSuccess: refreshConfigQueries,
  });

  const validateConfigMutation = useMutation({
    mutationFn: (versionId: string) => validateConfigVersion(serverId ?? '', versionId),
    onSuccess: refreshConfigQueries,
  });

  const applyConfigMutation = useMutation({
    mutationFn: (versionId: string) => applyConfigVersion(serverId ?? '', versionId),
    onSuccess: refreshConfigQueries,
  });

  const updateServerMutation = useMutation({
    mutationFn: () => {
      const currentServer = serverQuery.data;
      if (!currentServer) {
        throw new Error('Server data is unavailable.');
      }

      const nextName = editName.trim();
      const nextProvider = editProvider.trim();
      const nextLocation = editLocation.trim();
      const nextPublicIp = editPublicIp.trim();
      const nextDescription = editDescription.trim();

      return updateServer(serverId ?? '', {
        ...(nextName !== currentServer.name ? { name: nextName } : {}),
        ...(nextProvider !== (currentServer.provider ?? '') ? { provider: nextProvider } : {}),
        ...(nextLocation !== (currentServer.location ?? '') ? { location: nextLocation } : {}),
        ...(nextPublicIp !== (currentServer.publicIp ?? '') ? { publicIp: nextPublicIp } : {}),
        ...(nextDescription !== (currentServer.description ?? '')
          ? { description: nextDescription }
          : {}),
      });
    },
    onSuccess: async () => {
      setIsEditing(false);
      setHasSubmittedEdit(false);
      setUpdateSucceeded(true);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['server', serverId] }),
        queryClient.invalidateQueries({ queryKey: ['servers'] }),
      ]);
    },
  });

  const deleteServerMutation = useMutation({
    mutationFn: () => deleteServer(serverId ?? ''),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['servers'] });
      navigate('/servers', { replace: true, state: { serverDeleted: true } });
    },
  });

  if (!serverId) {
    return (
      <section className="page">
        <h1>{t('serverDetails.notFoundTitle')}</h1>
        <p className="muted-text">{t('serverDetails.noServerId')}</p>
        <Link className="text-link" to="/servers">{t('serverDetails.backToServers')}</Link>
      </section>
    );
  }

  const isNotFound =
    serverQuery.error instanceof ApiError && serverQuery.error.status === 404;

  if (serverQuery.isLoading) {
    return (
      <section className="page">
        <p className="muted-text">{t('serverDetails.loading')}</p>
      </section>
    );
  }

  if (serverQuery.isError) {
    return (
      <section className="page">
        <h1>{isNotFound ? t('serverDetails.notFoundTitle') : t('serverDetails.unavailableTitle')}</h1>
        <p className="muted-text">
          {isNotFound
            ? t('serverDetails.requestedNotFound')
            : t('serverDetails.loadError')}
        </p>
        <Link className="text-link" to="/servers">{t('serverDetails.backToServers')}</Link>
      </section>
    );
  }

  const server = serverQuery.data;

  if (!server) {
    return (
      <section className="page">
        <h1>{t('serverDetails.unavailableTitle')}</h1>
        <p className="muted-text">{t('serverDetails.loadError')}</p>
        <Link className="text-link" to="/servers">{t('serverDetails.backToServers')}</Link>
      </section>
    );
  }

  const agent = server.agent;
  const editNameIsValid = editName.trim() !== '';
  const editPublicIpIsValid = isValidIpAddress(editPublicIp);
  const publicIpChanged = editPublicIp.trim() !== (server.publicIp ?? '');
  const hasServerChanges =
    editName.trim() !== server.name
    || editProvider.trim() !== (server.provider ?? '')
    || editLocation.trim() !== (server.location ?? '')
    || publicIpChanged
    || editDescription.trim() !== (server.description ?? '');
  const canSaveServer =
    hasServerChanges
    && editNameIsValid
    && editPublicIpIsValid
    && !updateServerMutation.isPending;
  const canDeleteServer = deleteConfirmation === server.name && !deleteServerMutation.isPending;
  const routingProfiles = routingProfilesQuery.data?.items ?? [];
  const assignedRoutingProfile = serverRoutingProfileQuery.data?.routingProfile ?? null;
  const canSaveRoutingProfile = selectedRoutingProfileId.trim() !== '';
  const configVersions = configVersionsQuery.data?.items ?? [];
  const applyJobs = applyJobsQuery.data?.items ?? [];
  const versionsById = new Map(configVersions.map((version) => [version.id, version]));
  const configSnippet = registrationToken
    ? `manager_url: "http://localhost:8080"\nregistration_token: "${registrationToken.registrationToken}"\nheartbeat_interval_seconds: 30`
    : '';

  return (
    <section className="page server-details-page">
      <Link className="text-link" to="/servers">{t('serverDetails.backToServersArrow')}</Link>

      {wasJustCreated && (
        <div className="form-message form-message-success" role="status">
          {t('servers.createSuccess')}
        </div>
      )}

      {updateSucceeded && (
        <div className="form-message form-message-success" role="status">
          {t('serverDetails.editSuccess')}
        </div>
      )}

      <div className="page-header server-details-header">
        <div>
          <h1>{formatValue(server.name)}</h1>
          <p>{formatValue(server.description)}</p>
        </div>

        <StatusBadge status={server.status} />
      </div>

      <div className="details-layout">
        <div className="panel">
          <div className="panel-header">
            <div className="panel-title">{t('serverDetails.detailsTitle')}</div>
            {!isEditing && (
              <button
                className="small-button"
                type="button"
                onClick={() => {
                  setEditName(server.name);
                  setEditProvider(server.provider ?? '');
                  setEditLocation(server.location ?? '');
                  setEditPublicIp(server.publicIp ?? '');
                  setEditDescription(server.description ?? '');
                  setHasSubmittedEdit(false);
                  setUpdateSucceeded(false);
                  updateServerMutation.reset();
                  setIsEditing(true);
                }}
              >
                {t('serverDetails.editServer')}
              </button>
            )}
          </div>

          {isEditing ? (
            <form
              className="server-edit-form"
              noValidate
              onSubmit={(event) => {
                event.preventDefault();
                setHasSubmittedEdit(true);
                if (canSaveServer) {
                  updateServerMutation.mutate();
                }
              }}
            >
              <div className="server-edit-grid">
                <label className="field">
                  <span>{t('servers.name')}</span>
                  <input
                    autoFocus
                    required
                    value={editName}
                    onChange={(event) => setEditName(event.target.value)}
                    aria-invalid={hasSubmittedEdit && !editNameIsValid}
                  />
                  {hasSubmittedEdit && !editNameIsValid && (
                    <small className="field-error">
                      {t('serverDetails.editValidationNameRequired')}
                    </small>
                  )}
                </label>

                <label className="field">
                  <span>{t('servers.publicIp')}</span>
                  <input
                    required
                    value={editPublicIp}
                    onChange={(event) => setEditPublicIp(event.target.value)}
                    aria-invalid={hasSubmittedEdit && !editPublicIpIsValid}
                  />
                  {hasSubmittedEdit && !editPublicIpIsValid && (
                    <small className="field-error">
                      {t('serverDetails.editValidationPublicIpInvalid')}
                    </small>
                  )}
                </label>

                <label className="field">
                  <span>{t('servers.provider')}</span>
                  <input
                    value={editProvider}
                    onChange={(event) => setEditProvider(event.target.value)}
                  />
                </label>

                <label className="field">
                  <span>{t('servers.location')}</span>
                  <input
                    value={editLocation}
                    onChange={(event) => setEditLocation(event.target.value)}
                  />
                </label>

                <label className="field server-edit-description">
                  <span>{t('servers.description')}</span>
                  <input
                    value={editDescription}
                    onChange={(event) => setEditDescription(event.target.value)}
                  />
                </label>
              </div>

              {publicIpChanged && (
                <div className="form-message form-message-warning" role="status">
                  {t('serverDetails.editPublicIpWarning')}
                </div>
              )}

              {updateServerMutation.isError && (
                <div className="form-message form-message-error" role="alert">
                  {getUpdateErrorMessage(updateServerMutation.error)}
                </div>
              )}

              <div className="form-actions">
                <button
                  className="primary-button"
                  type="submit"
                  disabled={updateServerMutation.isPending || !hasServerChanges}
                >
                  {updateServerMutation.isPending
                    ? t('serverDetails.editSaving')
                    : t('serverDetails.editSave')}
                </button>
                <button
                  className="small-button"
                  type="button"
                  disabled={updateServerMutation.isPending}
                  onClick={() => {
                    setIsEditing(false);
                    setHasSubmittedEdit(false);
                    updateServerMutation.reset();
                  }}
                >
                  {t('common.cancel')}
                </button>
              </div>
            </form>
          ) : (
            <div className="detail-list">
              <DetailRow label={t('serverDetails.labelName')}>{formatValue(server.name)}</DetailRow>
              <DetailRow label={t('serverDetails.labelDescription')}>{formatValue(server.description)}</DetailRow>
              <DetailRow label={t('serverDetails.labelProvider')}>{formatValue(server.provider)}</DetailRow>
              <DetailRow label={t('serverDetails.labelLocation')}>{formatValue(server.location)}</DetailRow>
              <DetailRow label={t('serverDetails.labelPublicIp')}>{formatValue(server.publicIp)}</DetailRow>
              <DetailRow label={t('serverDetails.labelPrivateIp')}>{formatValue(server.privateIp)}</DetailRow>
              <DetailRow label={t('serverDetails.labelServerStatus')}><StatusBadge status={server.status} /></DetailRow>
              <DetailRow label={t('serverDetails.labelCreatedAt')}>{formatDate(server.createdAt)}</DetailRow>
              <DetailRow label={t('serverDetails.labelUpdatedAt')}>{formatDate(server.updatedAt)}</DetailRow>
            </div>
          )}
        </div>

        <div className="panel">
          <div className="panel-title">{t('servers.agent')}</div>
          {agent ? (
            <div className="detail-list">
              <DetailRow label={t('serverDetails.labelAgentId')}>{formatValue(agent.id)}</DetailRow>
              <DetailRow label={t('serverDetails.labelHostname')}>{formatValue(agent.hostname)}</DetailRow>
              <DetailRow label="OS">{formatValue(agent.os)}</DetailRow>
              <DetailRow label={t('serverDetails.labelArch')}>{formatValue(agent.arch)}</DetailRow>
              <DetailRow label={t('serverDetails.labelAgentVersion')}>{formatValue(agent.agentVersion)}</DetailRow>
              <DetailRow label={t('serverDetails.labelAgentStatus')}><StatusBadge status={agent.status} /></DetailRow>
              <DetailRow label={t('serverDetails.labelCapabilities')}>
                <pre className="inline-code">{formatCapabilities(agent.capabilities)}</pre>
              </DetailRow>
              <DetailRow label={t('serverDetails.labelLastSeenAt')}>{formatDate(agent.lastSeenAt)}</DetailRow>
            </div>
          ) : (
            <div className="empty-state empty-state-card agent-registration-empty-state">
              <strong>{t('serverDetails.noAgent')}</strong>
              <span>{t('serverDetails.noAgentDescription')}</span>
              <button
                className="primary-button"
                type="button"
                disabled={registrationTokenMutation.isPending || !serverId}
                onClick={() => createRegistrationToken(true)}
              >
                {registrationTokenMutation.isPending
                  ? t('serverDetails.generating')
                  : t('serverDetails.createRegistrationToken')}
              </button>
              {registrationTokenMutation.isError && openRegistrationTokenDialogOnSuccess.current && (
                <div className="form-message form-message-error" role="alert">
                  {t('serverDetails.registrationTokenError')}
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      <div className="panel routing-profile-assignment-panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">{t('serverDetails.routingProfileTitle')}</div>
            <p className="panel-subtitle">{t('serverDetails.routingProfileSubtitle')}</p>
          </div>
          {assignedRoutingProfile ? <StatusBadge status={assignedRoutingProfile.isDefault ? 'default' : 'custom'} /> : <StatusBadge status="unassigned" />}
        </div>

        {serverRoutingProfileQuery.isError && (
          <div className="form-message form-message-error">{t('serverDetails.routingAssignmentLoadError')}</div>
        )}
        {routingProfilesQuery.isError && (
          <div className="form-message form-message-error">{t('serverDetails.routingProfilesLoadError')}</div>
        )}
        {(assignRoutingProfileMutation.isError || clearRoutingProfileMutation.isError) && (
          <div className="form-message form-message-error">{t('serverDetails.routingAssignmentUpdateError')}</div>
        )}

        <div className="routing-profile-assignment-current">
          <DetailRow label={t('serverDetails.currentProfile')}>{assignedRoutingProfile ? formatValue(assignedRoutingProfile.name) : t('serverDetails.noExplicitAssignment')}</DetailRow>
          <DetailRow label={t('serverDetails.labelDescription')}>{formatValue(assignedRoutingProfile?.description)}</DetailRow>
          <DetailRow label={t('serverDetails.assignedAt')}>{formatDate(serverRoutingProfileQuery.data?.createdAt)}</DetailRow>
          <DetailRow label={t('serverDetails.labelUpdatedAt')}>{formatDate(serverRoutingProfileQuery.data?.updatedAt)}</DetailRow>
        </div>

        <form
          className="routing-profile-assignment-form"
          onSubmit={(event) => {
            event.preventDefault();
            assignRoutingProfileMutation.mutate();
          }}
        >
          <label className="field">
            <span>{t('serverDetails.routingProfileTitle')}</span>
            <select
              value={selectedRoutingProfileId}
              onChange={(event) => setSelectedRoutingProfileId(event.target.value)}
            >
              <option value="">{t('serverDetails.selectRoutingProfile')}</option>
              {routingProfiles.map((profile) => (
                <option value={profile.id} key={profile.id}>
                  {profile.name}{profile.isDefault ? ' (default)' : ''}
                </option>
              ))}
            </select>
          </label>

          <div className="routing-profile-assignment-actions">
            <button
              className="small-button"
              type="submit"
              disabled={!canSaveRoutingProfile || assignRoutingProfileMutation.isPending}
            >
              {assignRoutingProfileMutation.isPending ? t('serverDetails.saving') : t('serverDetails.saveAssignment')}
            </button>
            <button
              className="small-button"
              type="button"
              disabled={clearRoutingProfileMutation.isPending || !assignedRoutingProfile}
              onClick={() => clearRoutingProfileMutation.mutate()}
            >
              {clearRoutingProfileMutation.isPending ? t('serverDetails.clearing') : t('serverDetails.clearAssignment')}
            </button>
          </div>
        </form>
      </div>

      <div className="panel token-panel">
        <div className="panel-title">{t('serverDetails.registrationTokenTitle')}</div>
        <p className="muted-text">
          {t('serverDetails.registrationTokenSubtitle')}
        </p>
        <button
          className="primary-button"
          type="button"
          disabled={registrationTokenMutation.isPending || !serverId}
          onClick={() => createRegistrationToken(false)}
        >
          {registrationTokenMutation.isPending ? t('serverDetails.generating') : t('serverDetails.generateRegistrationToken')}
        </button>

        {registrationTokenMutation.isError && !openRegistrationTokenDialogOnSuccess.current && (
          <div className="form-message form-message-error" role="alert">{t('serverDetails.registrationTokenError')}</div>
        )}

        {registrationToken && (
          <RegistrationTokenResult
            registrationToken={registrationToken}
            configSnippet={configSnippet}
          />
        )}
      </div>

      <div className="panel admin-table-panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">{t('serverDetails.configVersions')}</div>
            <p className="panel-subtitle">{t('serverDetails.configVersionsSubtitle')}</p>
          </div>
          <button
            className="small-button"
            type="button"
            disabled={renderConfigMutation.isPending}
            onClick={() => renderConfigMutation.mutate()}
          >
            {renderConfigMutation.isPending ? t('serverDetails.rendering') : t('serverDetails.renderConfig')}
          </button>
        </div>

        {configVersionsQuery.isError && (
          <div className="form-message form-message-error">{t('serverDetails.configVersionsLoadError')}</div>
        )}

        {renderConfigMutation.isError && (
          <div className="form-message form-message-error">{t('serverDetails.renderConfigError')}</div>
        )}

        {(validateConfigMutation.isError || applyConfigMutation.isError) && (
          <div className="form-message form-message-error">
            {t('serverDetails.configActionError')}
          </div>
        )}

        {configVersionsQuery.isLoading ? (
          <p className="empty-state">{t('serverDetails.loadingConfigVersions')}</p>
        ) : configVersions.length === 0 ? (
          <p className="empty-state">{t('serverDetails.noConfigVersions')}</p>
        ) : (
          <div className="admin-table config-versions-table">
            <div className="admin-table-row admin-table-head config-versions-table-row">
              <span>{t('serverDetails.version')}</span>
              <span>{t('vpnAccounts.status')}</span>
              <span>{t('serverDetails.hash')}</span>
              <span>{t('serverDetails.created')}</span>
              <span>{t('serverDetails.applied')}</span>
              <span>{t('routingProfiles.actions')}</span>
            </div>
            {configVersions.map((version: ConfigVersion) => {
              const isValidating =
                validateConfigMutation.isPending && validateConfigMutation.variables === version.id;
              const isApplying =
                applyConfigMutation.isPending && applyConfigMutation.variables === version.id;

              return (
                <div className="admin-table-row config-versions-table-row" key={version.id}>
                  <strong>v{version.version}</strong>
                  <StatusBadge status={version.status} />
                  <code>{shortHash(version.configHash)}</code>
                  <span>{formatDate(version.createdAt)}</span>
                  <span>{formatDate(version.appliedAt)}</span>
                  <div className="table-actions">
                    <button
                      className="small-button"
                      type="button"
                      disabled={isValidating}
                      onClick={() => validateConfigMutation.mutate(version.id)}
                    >
                      {isValidating ? t('serverDetails.validating') : t('serverDetails.validate')}
                    </button>
                    <button
                      className="small-button"
                      type="button"
                      disabled={version.status !== 'validated' || isApplying}
                      onClick={() => applyConfigMutation.mutate(version.id)}
                    >
                      {isApplying ? t('serverDetails.applying') : t('serverDetails.apply')}
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      <div className="panel admin-table-panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">{t('serverDetails.applyJobs')}</div>
            <p className="panel-subtitle">{t('serverDetails.applyJobsSubtitle')}</p>
          </div>
        </div>

        {applyJobsQuery.isError && (
          <div className="form-message form-message-error">{t('serverDetails.applyJobsLoadError')}</div>
        )}

        {applyJobsQuery.isLoading ? (
          <p className="empty-state">{t('serverDetails.loadingApplyJobs')}</p>
        ) : applyJobs.length === 0 ? (
          <p className="empty-state">{t('serverDetails.noApplyJobs')}</p>
        ) : (
          <div className="admin-table apply-jobs-table">
            <div className="admin-table-row admin-table-head apply-jobs-table-row">
              <span>{t('vpnAccounts.status')}</span>
              <span>{t('routingProfiles.action')}</span>
              <span>{t('serverDetails.version')}</span>
              <span>{t('serverDetails.stages')}</span>
              <span>{t('serverDetails.error')}</span>
              <span>{t('serverDetails.timestamps')}</span>
            </div>
            {applyJobs.map((job: ConfigApplyJob) => {
              const version = versionsById.get(job.configVersionId);

              return (
                <div className="admin-table-row apply-jobs-table-row" key={job.id}>
                  <StatusBadge status={job.status} />
                  <strong>{job.action}</strong>
                  <div className="timestamp-stack">
                    <strong>{version ? `v${version.version}` : t('serverDetails.versionUnknown')}</strong>
                    <span>{shortHash(job.configVersionId)}</span>
                  </div>
                  <StageSummary resultPayload={job.resultPayload} />
                  <span>{formatValue(job.errorMessage)}</span>
                  <div className="timestamp-stack">
                    <span>{t('serverDetails.createdValue', { value: formatDate(job.createdAt) })}</span>
                    <span>{t('serverDetails.updatedValue', { value: formatDate(job.updatedAt) })}</span>
                    <span>{t('serverDetails.completedValue', { value: formatDate(job.completedAt) })}</span>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      <div className="panel server-danger-zone">
        <div>
          <div className="panel-title">{t('serverDetails.dangerZoneTitle')}</div>
          <p className="panel-subtitle">{t('serverDetails.dangerZoneSubtitle')}</p>
        </div>
        <button
          className="danger-button"
          type="button"
          onClick={() => {
            setDeleteConfirmation('');
            deleteServerMutation.reset();
            setIsDeleteOpen(true);
          }}
        >
          {t('serverDetails.deleteServer')}
        </button>
      </div>

      {isRegistrationTokenDialogOpen && registrationToken && (
        <div
          className="subscription-qr-modal-backdrop"
          onClick={() => setIsRegistrationTokenDialogOpen(false)}
        >
          <div
            className="subscription-qr-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="registration-token-dialog-title"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="subscription-qr-modal-header">
              <h3 id="registration-token-dialog-title">{t('serverDetails.registrationTokenTitle')}</h3>
              <button
                className="registration-token-dialog-close"
                type="button"
                aria-label={t('common.close')}
                title={t('common.close')}
                onClick={() => setIsRegistrationTokenDialogOpen(false)}
              >
                ×
              </button>
            </div>
            <div className="subscription-qr-modal-body">
              <RegistrationTokenResult
                registrationToken={registrationToken}
                configSnippet={configSnippet}
                onCopy={() => void copyRegistrationToken()}
                isCopied={isRegistrationTokenCopied}
                isConfigCollapsible
              />
            </div>
          </div>
        </div>
      )}

      {isDeleteOpen && (
        <div className="server-delete-modal-backdrop">
          <div
            className="server-delete-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="delete-server-title"
          >
            <div>
              <h2 id="delete-server-title">{t('serverDetails.deleteTitle')}</h2>
              <p>{t('serverDetails.deleteWarning', { name: server.name })}</p>
            </div>

            <label className="field">
              <span>{t('serverDetails.deleteConfirmationLabel', { name: server.name })}</span>
              <input
                autoFocus
                value={deleteConfirmation}
                onChange={(event) => setDeleteConfirmation(event.target.value)}
                disabled={deleteServerMutation.isPending}
              />
            </label>

            {deleteServerMutation.isError && (
              <div className="form-message form-message-error" role="alert">
                {getDeleteErrorMessage(deleteServerMutation.error)}
              </div>
            )}

            <div className="form-actions">
              <button
                className="danger-button"
                type="button"
                disabled={!canDeleteServer}
                onClick={() => deleteServerMutation.mutate()}
              >
                {deleteServerMutation.isPending
                  ? t('serverDetails.deleting')
                  : t('serverDetails.confirmDelete')}
              </button>
              <button
                className="small-button"
                type="button"
                disabled={deleteServerMutation.isPending}
                onClick={() => {
                  setIsDeleteOpen(false);
                  setDeleteConfirmation('');
                  deleteServerMutation.reset();
                }}
              >
                {t('common.cancel')}
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
