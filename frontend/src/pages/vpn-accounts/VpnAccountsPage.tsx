import { type FormEvent, useEffect, useState, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { getServers } from '../../entities/server/api/serverApi';
import {
  createVpnAccount,
  getVpnAccountCredentials,
  getVpnAccounts,
  type VpnAccount,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { t } from '../../shared/i18n/i18n';
import { EmptyState } from '../../shared/ui/EmptyState';
import { StatusBadge } from '../../shared/ui/StatusBadge';
import { TrafficStatsPanel } from './TrafficStatsPanel';
import { VpnClientConnectionPanel } from './VpnClientConnectionPanel';

function formatDate(value?: string | null): string {
  if (!value) {
    return t('common.notAvailable');
  }

  const date = new Date(value);

  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatValue(value?: string | null): string {
  return value && value.trim() !== '' ? value : t('common.notAvailable');
}

function getErrorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() !== '' ? error.message : fallback;
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="detail-row">
      <span>{label}</span>
      <strong>{children}</strong>
    </div>
  );
}

function VpnAccountRow({ account, selected }: { account: VpnAccount; selected: boolean }) {
  return (
    <Link
      className={`admin-table-row vpn-accounts-table-row vpn-account-row-link${selected ? ' vpn-account-row-selected' : ''}`}
      to={`/vpn-accounts/${account.id}`}
    >
      <div className="portal-profile-cell">
        <strong className="portal-profile-name">{formatValue(account.displayName)}</strong>
        <span className="portal-profile-meta">{formatValue(account.email)}</span>
      </div>
      <StatusBadge status={account.status} />
      <span>{formatValue(account.serverId)}</span>
      <code>{formatValue(account.vlessUuid)}</code>
      <span>{formatDate(account.expiresAt)}</span>
    </Link>
  );
}

export function VpnAccountsPage() {
  const { accountId } = useParams<{ accountId: string }>();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const [isCreateOpen, setIsCreateOpen] = useState(searchParams.get('create') === '1');
  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');
  const [serverId, setServerId] = useState('');

  useEffect(() => {
    if (searchParams.get('create') === '1') {
      setIsCreateOpen(true);
    }
  }, [searchParams]);

  const accountsQuery = useQuery({
    queryKey: ['vpn-accounts'],
    queryFn: getVpnAccounts,
  });

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
  });

  const credentialsQuery = useQuery({
    queryKey: ['vpn-account-credentials', accountId],
    queryFn: () => getVpnAccountCredentials(accountId ?? ''),
    enabled: Boolean(accountId),
  });

  const createAccountMutation = useMutation({
    mutationFn: () => createVpnAccount({
      displayName: displayName.trim(),
      email: email.trim() || undefined,
      serverId: serverId || undefined,
    }),
    onSuccess: async (account) => {
      setDisplayName('');
      setEmail('');
      setServerId('');
      setIsCreateOpen(false);
      setSearchParams((current) => {
        current.delete('create');
        return current;
      }, { replace: true });
      await queryClient.invalidateQueries({ queryKey: ['vpn-accounts'] });
      navigate(`/vpn-accounts/${account.id}`);
    },
  });

  const accounts = accountsQuery.data?.items ?? [];
  const selectedAccount = accounts.find((account) => account.id === accountId);
  const credentials = credentialsQuery.data;
  const canCreateAccount = displayName.trim() !== '';

  function openCreateForm() {
    setIsCreateOpen(true);
    setSearchParams((current) => {
      current.set('create', '1');
      return current;
    }, { replace: true });
  }

  function closeCreateForm() {
    setIsCreateOpen(false);
    setSearchParams((current) => {
      current.delete('create');
      return current;
    }, { replace: true });
  }

  function handleCreateAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canCreateAccount) {
      return;
    }
    createAccountMutation.mutate();
  }

  return (
    <section className="page vpn-accounts-page feature-screen-page" style={{ overflowX: 'hidden' }}>
      <div className="page-header feature-page-header">
        <div>
          <h1>{t('vpnAccounts.title')}</h1>
          <p>{t('vpnAccounts.subtitle')}</p>
        </div>

        <div className="page-header-actions">
          <button className="primary-button" type="button" onClick={openCreateForm}>
            {t('vpnAccounts.createAction')}
          </button>
          <div className="status-pill">
            <span className="status-dot status-dot-ok" />
            {t('vpnAccounts.accountCount', { count: accounts.length })}
          </div>
        </div>
      </div>

      <div className="vpn-accounts-layout feature-screen-layout">
        <div className="panel admin-table-panel feature-list-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">{t('vpnAccounts.accountsPanelTitle')}</div>
              <p className="panel-subtitle">{t('vpnAccounts.accountsPanelSubtitle')}</p>
            </div>
            <button className="small-button" type="button" onClick={openCreateForm}>
              {t('vpnAccounts.createAction')}
            </button>
          </div>

          {isCreateOpen && (
            <form className="vpn-account-create-form" onSubmit={handleCreateAccount}>
              <div className="vpn-account-create-grid">
                <label className="field">
                  <span>{t('vpnAccounts.displayName')}</span>
                  <input
                    value={displayName}
                    onChange={(event) => setDisplayName(event.target.value)}
                    placeholder={t('vpnAccounts.displayNamePlaceholder')}
                  />
                </label>
                <label className="field">
                  <span>{t('vpnAccounts.email')}</span>
                  <input
                    type="email"
                    value={email}
                    onChange={(event) => setEmail(event.target.value)}
                    placeholder={t('vpnAccounts.emailPlaceholder')}
                  />
                </label>
                <label className="field">
                  <span>{t('vpnAccounts.serverAssignment')}</span>
                  <select value={serverId} onChange={(event) => setServerId(event.target.value)}>
                    <option value="">{t('vpnAccounts.noServerAssignment')}</option>
                    {(serversQuery.data?.items ?? []).map((server) => (
                      <option value={server.id} key={server.id}>
                        {server.name || server.id}
                      </option>
                    ))}
                  </select>
                </label>
              </div>
              {createAccountMutation.isError && (
                <div className="form-message form-message-error">
                  {getErrorMessage(createAccountMutation.error, t('vpnAccounts.createError'))}
                </div>
              )}
              <div className="form-actions">
                <button className="primary-button" type="submit" disabled={!canCreateAccount || createAccountMutation.isPending}>
                  {createAccountMutation.isPending ? t('vpnAccounts.creating') : t('vpnAccounts.createAction')}
                </button>
                <button className="small-button" type="button" onClick={closeCreateForm}>
                  {t('common.cancel')}
                </button>
              </div>
            </form>
          )}

          {accountsQuery.isLoading && <p className="empty-state">{t('common.loading')}</p>}

          {accountsQuery.isError && (
            <div className="form-message form-message-error">{t('vpnAccounts.loadError')}</div>
          )}

          {accountsQuery.isSuccess && accounts.length === 0 && (
            <div className="empty-state-with-action">
              <EmptyState
                title={t('vpnAccounts.emptyTitle')}
                description={t('vpnAccounts.emptyDescription')}
              />
              <button className="primary-button" type="button" onClick={openCreateForm}>
                {t('vpnAccounts.createAction')}
              </button>
            </div>
          )}

          {accounts.length > 0 && (
            <div className="admin-table vpn-accounts-table">
              <div className="admin-table-row admin-table-head vpn-accounts-table-row">
                <span>{t('vpnAccounts.account')}</span>
                <span>{t('vpnAccounts.status')}</span>
                <span>{t('vpnAccounts.server')}</span>
                <span>{t('vpnAccounts.vlessUuid')}</span>
                <span>{t('vpnAccounts.expires')}</span>
              </div>
              {accounts.map((account) => (
                <VpnAccountRow
                  account={account}
                  key={account.id}
                  selected={account.id === accountId}
                />
              ))}
            </div>
          )}
        </div>

        <div className="panel credentials-panel feature-detail-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">{t('vpnAccounts.credentialsTitle')}</div>
              <p className="panel-subtitle">{t('vpnAccounts.credentialsSubtitle')}</p>
            </div>
          </div>

          {!accountId && (
            <EmptyState
              title={t('vpnAccounts.selectTitle')}
              description={t('vpnAccounts.selectDescription')}
            />
          )}

          {accountId && accountsQuery.isSuccess && !selectedAccount && (
            <div className="form-message form-message-error">{t('vpnAccounts.selectedMissing')}</div>
          )}

          {credentialsQuery.isLoading && (
            <p className="empty-state">{t('vpnAccounts.loadingCredentials')}</p>
          )}

          {credentialsQuery.isError && (
            <div className="form-message form-message-error">{t('vpnAccounts.credentialsLoadError')}</div>
          )}

          {credentials && (
            <div className="detail-list credentials-detail-list feature-detail-list">
              <DetailRow label={t('vpnAccounts.accountId')}>{formatValue(credentials.vpnAccountId)}</DetailRow>
              <DetailRow label={t('vpnAccounts.serverId')}>{formatValue(credentials.serverId)}</DetailRow>
              <DetailRow label={t('vpnAccounts.protocol')}>{formatValue(credentials.protocol)}</DetailRow>
              <DetailRow label={t('vpnAccounts.endpoint')}>{formatValue(credentials.endpoint)}</DetailRow>
              <DetailRow label={t('vpnAccounts.vlessUuid')}>
                <code>{formatValue(credentials.vless.uuid)}</code>
              </DetailRow>
              <DetailRow label={t('vpnAccounts.flow')}>{formatValue(credentials.vless.flow)}</DetailRow>
              <DetailRow label={t('vpnAccounts.network')}>{formatValue(credentials.vless.network)}</DetailRow>
              <DetailRow label={t('vpnAccounts.realityEnabled')}>
                {credentials.reality.enabled ? t('vpnAccounts.enabled') : t('vpnAccounts.disabled')}
              </DetailRow>
              <DetailRow label={t('vpnAccounts.realityPublicKey')}>
                <code>{formatValue(credentials.reality.publicKey)}</code>
              </DetailRow>
              <DetailRow label={t('vpnAccounts.realityShortId')}>
                <code>{formatValue(credentials.reality.shortId)}</code>
              </DetailRow>
              <DetailRow label={t('vpnAccounts.realityServerName')}>
                {formatValue(credentials.reality.serverName)}
              </DetailRow>
            </div>
          )}
        </div>

        {accountId && selectedAccount && <TrafficStatsPanel accountId={accountId} />}

        {accountId && selectedAccount && <VpnClientConnectionPanel accountId={accountId} />}
      </div>
    </section>
  );
}
