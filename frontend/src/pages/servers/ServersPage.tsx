import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import {
  createServer,
  getServers,
  type Server,
} from '../../entities/server/api/serverApi';
import { ApiError } from '../../shared/api/client';
import { t } from '../../shared/i18n/i18n';
import { EmptyState } from '../../shared/ui/EmptyState';
import { StatusBadge } from '../../shared/ui/StatusBadge';

function formatDate(value?: string | null): string {
  if (!value) {
    return t('common.notAvailable');
  }

  return new Date(value).toLocaleString();
}

function formatValue(value?: string | null): string {
  return value && value.trim() !== '' ? value : t('common.notAvailable');
}

function deploymentRoleLabel(role: Server['deploymentRole']): string {
  switch (role) {
    case 'management':
      return t('servers.deploymentRole.management');
    case 'hybrid':
      return t('servers.deploymentRole.hybrid');
    default:
      return t('servers.deploymentRole.vpn');
  }
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

function getCreateErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === 'name_required') {
      return t('servers.validationNameRequired');
    }
    if (error.status === 400) {
      return t('servers.validationPublicIpInvalid');
    }
    if (error.status === 403) {
      return t('servers.createPermissionError');
    }
  }

  return t('servers.createError');
}

function ServerRow({ server }: { server: Server }) {
  return (
    <Link className="table-row servers-table-row table-row-link" to={`/servers/${server.id}`}>
      <div>
        <strong className="text-link">{formatValue(server.name)}</strong>
        <span>{formatValue(server.description)}</span>
      </div>
      <div>{deploymentRoleLabel(server.deploymentRole)}</div>
      <div>{formatValue(server.provider)}</div>
      <div>{formatValue(server.location)}</div>
      <div>{formatValue(server.publicIp)}</div>
      <div>
        <StatusBadge status={server.status} />
      </div>
      <div>
        {server.agent ? (
          <StatusBadge status={server.agent.status} />
        ) : (
          <span className="muted-text">{t('common.notAvailable')}</span>
        )}
      </div>
      <div>{formatValue(server.agent?.agentVersion)}</div>
      <div>{formatDate(server.agent?.lastSeenAt)}</div>
      <div>{formatDate(server.createdAt)}</div>
    </Link>
  );
}

export function ServersPage() {
  const navigate = useNavigate();
  const routeLocation = useLocation();
  const queryClient = useQueryClient();
  const wasJustDeleted = Boolean(
    (routeLocation.state as { serverDeleted?: boolean } | null)?.serverDeleted,
  );
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [hasSubmitted, setHasSubmitted] = useState(false);
  const [name, setName] = useState('');
  const [provider, setProvider] = useState('');
  const [location, setLocation] = useState('');
  const [publicIp, setPublicIp] = useState('');
  const [description, setDescription] = useState('');

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
  });

  const createServerMutation = useMutation({
    mutationFn: () => createServer({
      name: name.trim(),
      deploymentRole: 'vpn',
      provider: provider.trim() || undefined,
      location: location.trim() || undefined,
      publicIp: publicIp.trim(),
      description: description.trim() || undefined,
    }),
    onSuccess: async (server) => {
      await queryClient.invalidateQueries({ queryKey: ['servers'] });
      navigate(`/servers/${server.id}`, { state: { serverCreated: true } });
    },
  });

  const servers = serversQuery.data?.items ?? [];
  const nameIsValid = name.trim() !== '';
  const publicIpIsValid = isValidIpAddress(publicIp);
  const canCreateServer = nameIsValid && publicIpIsValid && !createServerMutation.isPending;

  function openCreateForm() {
    setHasSubmitted(false);
    createServerMutation.reset();
    setIsCreateOpen(true);
  }

  function closeCreateForm() {
    setIsCreateOpen(false);
    setHasSubmitted(false);
    setName('');
    setProvider('');
    setLocation('');
    setPublicIp('');
    setDescription('');
    createServerMutation.reset();
  }

  function handleCreateServer(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setHasSubmitted(true);

    if (!canCreateServer) {
      return;
    }

    createServerMutation.mutate();
  }

  return (
    <section className="page servers-page">
      <div className="page-header">
        <div>
          <h1>{t('servers.title')}</h1>
          <p>{t('servers.subtitle')}</p>
        </div>

        <div className="page-header-actions">
          <button className="primary-button" type="button" onClick={openCreateForm}>
            {t('servers.addAction')}
          </button>
          <div className="status-pill">
            <span className="status-dot status-dot-ok" />
            {servers.length} {t('servers.registered')}
          </div>
        </div>
      </div>

      {wasJustDeleted && (
        <div className="form-message form-message-success" role="status">
          {t('servers.deleteSuccess')}
        </div>
      )}

      <div className="panel table-panel servers-table-panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">{t('servers.panelTitle')}</div>
            <p className="panel-subtitle">{t('servers.panelSubtitle')}</p>
          </div>
          <button className="small-button" type="button" onClick={openCreateForm}>
            {t('servers.addAction')}
          </button>
        </div>

        {isCreateOpen && (
          <form className="server-create-form" onSubmit={handleCreateServer} noValidate>
            <div>
              <div className="panel-title">{t('servers.createTitle')}</div>
              <p className="panel-subtitle">{t('servers.createDescription')}</p>
            </div>

            <div className="server-create-grid">
              <label className="field">
                <span>{t('servers.name')}</span>
                <input
                  autoFocus
                  required
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder={t('servers.namePlaceholder')}
                  aria-invalid={hasSubmitted && !nameIsValid}
                />
                {hasSubmitted && !nameIsValid && (
                  <small className="field-error">{t('servers.validationNameRequired')}</small>
                )}
              </label>

              <label className="field">
                <span>{t('servers.publicIp')}</span>
                <input
                  required
                  value={publicIp}
                  onChange={(event) => setPublicIp(event.target.value)}
                  placeholder={t('servers.publicIpPlaceholder')}
                  aria-invalid={hasSubmitted && !publicIpIsValid}
                />
                {hasSubmitted && !publicIpIsValid && (
                  <small className="field-error">{t('servers.validationPublicIpInvalid')}</small>
                )}
              </label>

              <label className="field">
                <span>{t('servers.provider')}</span>
                <input
                  value={provider}
                  onChange={(event) => setProvider(event.target.value)}
                  placeholder={t('servers.providerPlaceholder')}
                />
              </label>

              <label className="field">
                <span>{t('servers.location')}</span>
                <input
                  value={location}
                  onChange={(event) => setLocation(event.target.value)}
                  placeholder={t('servers.locationPlaceholder')}
                />
              </label>

              <label className="field server-create-description">
                <span>{t('servers.description')}</span>
                <input
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                  placeholder={t('servers.descriptionPlaceholder')}
                />
              </label>
            </div>

            {createServerMutation.isError && (
              <div className="form-message form-message-error" role="alert">
                {getCreateErrorMessage(createServerMutation.error)}
              </div>
            )}

            <div className="form-actions">
              <button className="primary-button" type="submit" disabled={createServerMutation.isPending}>
                {createServerMutation.isPending ? t('servers.creating') : t('servers.createAction')}
              </button>
              <button
                className="small-button"
                type="button"
                onClick={closeCreateForm}
                disabled={createServerMutation.isPending}
              >
                {t('common.cancel')}
              </button>
            </div>
          </form>
        )}

        {serversQuery.isLoading && <p className="empty-state">{t('servers.loading')}</p>}

        {serversQuery.isError && (
          <div className="form-message form-message-error">{t('servers.loadError')}</div>
        )}

        {serversQuery.isSuccess && servers.length === 0 && (
          <div className="empty-state-with-action">
            <EmptyState title={t('servers.emptyTitle')} description={t('servers.emptyDescription')} />
            <button className="primary-button" type="button" onClick={openCreateForm}>
              {t('servers.addAction')}
            </button>
          </div>
        )}

        {servers.length > 0 && (
          <div className="table servers-table">
            <div className="table-row table-head servers-table-row">
              <div>{t('servers.name')}</div>
              <div>{t('servers.deploymentRole')}</div>
              <div>{t('servers.provider')}</div>
              <div>{t('servers.location')}</div>
              <div>{t('servers.publicIp')}</div>
              <div>{t('servers.status')}</div>
              <div>{t('servers.agent')}</div>
              <div>{t('servers.version')}</div>
              <div>{t('servers.lastSeen')}</div>
              <div>{t('servers.created')}</div>
            </div>

            {servers.map((server) => (
              <ServerRow server={server} key={server.id} />
            ))}
          </div>
        )}
      </div>
    </section>
  );
}
