import { useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { ApiError } from '../../shared/api/client';
import {
  createServerRegistrationToken,
  getServer,
  type RegistrationTokenResponse,
} from '../../entities/server/api/serverApi';

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

export function ServerDetailsPage() {
  const { serverId } = useParams<{ serverId: string }>();
  const [registrationToken, setRegistrationToken] = useState<RegistrationTokenResponse | null>(null);

  const serverQuery = useQuery({
    queryKey: ['server', serverId],
    queryFn: () => getServer(serverId ?? ''),
    enabled: Boolean(serverId),
  });

  const registrationTokenMutation = useMutation({
    mutationFn: () => createServerRegistrationToken(serverId ?? ''),
    onMutate: () => {
      setRegistrationToken(null);
    },
    onSuccess: (response) => {
      setRegistrationToken(response);
    },
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
    </section>
  );
}
