import { useEffect, useState, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { ApiError } from '../../shared/api/client';
import {
  applyConfigVersion,
  assignServerRoutingProfile,
  clearServerRoutingProfile,
  createServerRegistrationToken,
  getConfigApplyJobs,
  getConfigVersions,
  getServer,
  getServerRoutingProfile,
  renderConfig,
  validateConfigVersion,
  type ConfigApplyJob,
  type ConfigVersion,
  type RegistrationTokenResponse,
} from '../../entities/server/api/serverApi';
import { getRoutingProfiles } from '../../entities/routingProfile/api/routingProfileApi';

function formatDate(value?: string | null): string {
  if (!value) {
    return '—';
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
  const queryClient = useQueryClient();
  const [registrationToken, setRegistrationToken] = useState<RegistrationTokenResponse | null>(null);
  const [selectedRoutingProfileId, setSelectedRoutingProfileId] = useState('');

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
    },
    onSuccess: (response) => {
      setRegistrationToken(response);
    },
  });

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

  if (!serverId) {
    return (
      <section className="page">
        <h1>Server not found</h1>
        <p className="muted-text">No server ID was provided.</p>
        <Link className="text-link" to="/servers">Back to servers</Link>
      </section>
    );
  }

  const isNotFound =
    serverQuery.error instanceof ApiError && serverQuery.error.status === 404;

  if (serverQuery.isLoading) {
    return (
      <section className="page">
        <p className="muted-text">Loading server details...</p>
      </section>
    );
  }

  if (serverQuery.isError) {
    return (
      <section className="page">
        <h1>{isNotFound ? 'Server not found' : 'Server details unavailable'}</h1>
        <p className="muted-text">
          {isNotFound
            ? 'The requested server could not be found.'
            : 'Failed to load server details from Manager API.'}
        </p>
        <Link className="text-link" to="/servers">Back to servers</Link>
      </section>
    );
  }

  const server = serverQuery.data;

  if (!server) {
    return (
      <section className="page">
        <h1>Server details unavailable</h1>
        <p className="muted-text">Failed to load server details from Manager API.</p>
        <Link className="text-link" to="/servers">Back to servers</Link>
      </section>
    );
  }

  const agent = server.agent;
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
      <Link className="text-link" to="/servers">← Back to servers</Link>

      <div className="page-header server-details-header">
        <div>
          <h1>{formatValue(server.name)}</h1>
          <p>{formatValue(server.description)}</p>
        </div>

        <StatusBadge status={server.status} />
      </div>

      <div className="details-layout">
        <div className="panel">
          <div className="panel-title">Server details</div>
          <div className="detail-list">
            <DetailRow label="Name">{formatValue(server.name)}</DetailRow>
            <DetailRow label="Description">{formatValue(server.description)}</DetailRow>
            <DetailRow label="Provider">{formatValue(server.provider)}</DetailRow>
            <DetailRow label="Location">{formatValue(server.location)}</DetailRow>
            <DetailRow label="Public IP">{formatValue(server.publicIp)}</DetailRow>
            <DetailRow label="Private IP">{formatValue(server.privateIp)}</DetailRow>
            <DetailRow label="Server status"><StatusBadge status={server.status} /></DetailRow>
            <DetailRow label="Created at">{formatDate(server.createdAt)}</DetailRow>
            <DetailRow label="Updated at">{formatDate(server.updatedAt)}</DetailRow>
          </div>
        </div>

        <div className="panel">
          <div className="panel-title">Agent</div>
          {agent ? (
            <div className="detail-list">
              <DetailRow label="Agent ID">{formatValue(agent.id)}</DetailRow>
              <DetailRow label="Hostname">{formatValue(agent.hostname)}</DetailRow>
              <DetailRow label="OS">{formatValue(agent.os)}</DetailRow>
              <DetailRow label="Arch">{formatValue(agent.arch)}</DetailRow>
              <DetailRow label="Agent version">{formatValue(agent.agentVersion)}</DetailRow>
              <DetailRow label="Agent status"><StatusBadge status={agent.status} /></DetailRow>
              <DetailRow label="Capabilities">
                <pre className="inline-code">{formatCapabilities(agent.capabilities)}</pre>
              </DetailRow>
              <DetailRow label="Last seen at">{formatDate(agent.lastSeenAt)}</DetailRow>
            </div>
          ) : (
            <p className="empty-state">No agent registered yet.</p>
          )}
        </div>
      </div>

      <div className="panel routing-profile-assignment-panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">Routing profile</div>
            <p className="panel-subtitle">Assign the split-tunnel profile used when rendering this server config.</p>
          </div>
          {assignedRoutingProfile ? <StatusBadge status={assignedRoutingProfile.isDefault ? 'default' : 'custom'} /> : <StatusBadge status="unassigned" />}
        </div>

        {serverRoutingProfileQuery.isError && (
          <div className="form-message form-message-error">Failed to load server routing profile assignment.</div>
        )}
        {routingProfilesQuery.isError && (
          <div className="form-message form-message-error">Failed to load routing profiles.</div>
        )}
        {(assignRoutingProfileMutation.isError || clearRoutingProfileMutation.isError) && (
          <div className="form-message form-message-error">Failed to update routing profile assignment.</div>
        )}

        <div className="routing-profile-assignment-current">
          <DetailRow label="Current profile">{assignedRoutingProfile ? formatValue(assignedRoutingProfile.name) : 'No explicit assignment'}</DetailRow>
          <DetailRow label="Description">{formatValue(assignedRoutingProfile?.description)}</DetailRow>
          <DetailRow label="Assigned at">{formatDate(serverRoutingProfileQuery.data?.createdAt)}</DetailRow>
          <DetailRow label="Updated at">{formatDate(serverRoutingProfileQuery.data?.updatedAt)}</DetailRow>
        </div>

        <form
          className="routing-profile-assignment-form"
          onSubmit={(event) => {
            event.preventDefault();
            assignRoutingProfileMutation.mutate();
          }}
        >
          <label className="field">
            <span>Routing profile</span>
            <select
              value={selectedRoutingProfileId}
              onChange={(event) => setSelectedRoutingProfileId(event.target.value)}
            >
              <option value="">Select routing profile...</option>
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
              {assignRoutingProfileMutation.isPending ? 'Saving...' : 'Save assignment'}
            </button>
            <button
              className="small-button"
              type="button"
              disabled={clearRoutingProfileMutation.isPending || !assignedRoutingProfile}
              onClick={() => clearRoutingProfileMutation.mutate()}
            >
              {clearRoutingProfileMutation.isPending ? 'Clearing...' : 'Clear assignment'}
            </button>
          </div>
        </form>
      </div>

      <div className="panel token-panel">
        <div className="panel-title">Agent registration token</div>
        <p className="muted-text">
          Generate a one-time token for this server. The raw token is shown only once.
        </p>
        <button
          className="primary-button"
          type="button"
          disabled={registrationTokenMutation.isPending || !serverId}
          onClick={() => registrationTokenMutation.mutate()}
        >
          {registrationTokenMutation.isPending ? 'Generating...' : 'Generate registration token'}
        </button>

        {registrationTokenMutation.isError && (
          <div className="form-message form-message-error">Failed to generate registration token.</div>
        )}

        {registrationToken && (
          <div className="token-result">
            <div className="form-message form-message-warning">
              Save this token now. It is shown only once and is not stored by the frontend.
            </div>
            <DetailRow label="Registration token">
              <code>{registrationToken.registrationToken}</code>
            </DetailRow>
            <DetailRow label="Expires at">{formatDate(registrationToken.expiresAt)}</DetailRow>
            <div className="panel-title token-snippet-title">Example agent config</div>
            <pre className="code-block">{configSnippet}</pre>
          </div>
        )}
      </div>

      <div className="panel admin-table-panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">Config versions</div>
            <p className="panel-subtitle">Rendered configs for this server and their deploy state.</p>
          </div>
          <button
            className="small-button"
            type="button"
            disabled={renderConfigMutation.isPending}
            onClick={() => renderConfigMutation.mutate()}
          >
            {renderConfigMutation.isPending ? 'Rendering...' : 'Render config'}
          </button>
        </div>

        {configVersionsQuery.isError && (
          <div className="form-message form-message-error">Failed to load config versions.</div>
        )}

        {renderConfigMutation.isError && (
          <div className="form-message form-message-error">Failed to render config.</div>
        )}

        {(validateConfigMutation.isError || applyConfigMutation.isError) && (
          <div className="form-message form-message-error">
            Config action failed. Check server agent state and permissions.
          </div>
        )}

        {configVersionsQuery.isLoading ? (
          <p className="empty-state">Loading config versions...</p>
        ) : configVersions.length === 0 ? (
          <p className="empty-state">No config versions rendered yet.</p>
        ) : (
          <div className="admin-table config-versions-table">
            <div className="admin-table-row admin-table-head config-versions-table-row">
              <span>Version</span>
              <span>Status</span>
              <span>Hash</span>
              <span>Created</span>
              <span>Applied</span>
              <span>Actions</span>
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
                      {isValidating ? 'Validating...' : 'Validate'}
                    </button>
                    <button
                      className="small-button"
                      type="button"
                      disabled={version.status !== 'validated' || isApplying}
                      onClick={() => applyConfigMutation.mutate(version.id)}
                    >
                      {isApplying ? 'Applying...' : 'Apply'}
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
            <div className="panel-title">Apply jobs</div>
            <p className="panel-subtitle">Agent-side config deployment progress and results.</p>
          </div>
        </div>

        {applyJobsQuery.isError && (
          <div className="form-message form-message-error">Failed to load apply jobs.</div>
        )}

        {applyJobsQuery.isLoading ? (
          <p className="empty-state">Loading apply jobs...</p>
        ) : applyJobs.length === 0 ? (
          <p className="empty-state">No apply jobs have been queued yet.</p>
        ) : (
          <div className="admin-table apply-jobs-table">
            <div className="admin-table-row admin-table-head apply-jobs-table-row">
              <span>Status</span>
              <span>Action</span>
              <span>Version</span>
              <span>Stages</span>
              <span>Error</span>
              <span>Timestamps</span>
            </div>
            {applyJobs.map((job: ConfigApplyJob) => {
              const version = versionsById.get(job.configVersionId);

              return (
                <div className="admin-table-row apply-jobs-table-row" key={job.id}>
                  <StatusBadge status={job.status} />
                  <strong>{job.action}</strong>
                  <div className="timestamp-stack">
                    <strong>{version ? `v${version.version}` : 'Version unknown'}</strong>
                    <span>{shortHash(job.configVersionId)}</span>
                  </div>
                  <StageSummary resultPayload={job.resultPayload} />
                  <span>{formatValue(job.errorMessage)}</span>
                  <div className="timestamp-stack">
                    <span>Created {formatDate(job.createdAt)}</span>
                    <span>Updated {formatDate(job.updatedAt)}</span>
                    <span>Completed {formatDate(job.completedAt)}</span>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </section>
  );
}
