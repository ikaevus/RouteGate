import { type FormEvent, type ReactNode, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { getServers } from '../../entities/server/api/serverApi';
import {
  createVpnAccount,
  getVpnAccountCredentials,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { updateVpnAccountNotes } from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { t } from '../../shared/i18n/i18n';
import { TrafficStatsPanel } from './TrafficStatsPanel';
import { VpnAccountManagementList } from './VpnAccountManagementList';
import { VpnAccountManagementPanel } from './VpnAccountManagementPanel';
import { VpnClientConnectionPanel } from './VpnClientConnectionPanel';
import { getVpnAccountManagementCopy } from './vpnAccountManagementCopy';
import './vpnAccountManagement.css';

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

export function VpnAccountsPage() {
  const copy = getVpnAccountManagementCopy();
  const { accountId } = useParams<{ accountId: string }>();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const [isCreateOpen, setIsCreateOpen] = useState(searchParams.get('create') === '1');
  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');
  const [serverId, setServerId] = useState('');
  const [notes, setNotes] = useState('');

  useEffect(() => {
    if (searchParams.get('create') === '1') setIsCreateOpen(true);
  }, [searchParams]);

  const serversQuery = useQuery({ queryKey: ['servers'], queryFn: getServers });

  const credentialsQuery = useQuery({
    queryKey: ['vpn-account-credentials', accountId],
    queryFn: () => getVpnAccountCredentials(accountId ?? ''),
    enabled: Boolean(accountId),
  });

  const createAccountMutation = useMutation({
    mutationFn: async () => {
      const account = await createVpnAccount({
        displayName: displayName.trim(),
        email: email.trim() || undefined,
        serverId: serverId || undefined,
      });
      if (notes.trim()) {
        await updateVpnAccountNotes(account.id, notes);
      }
      return account;
    },
    onSuccess: async (account) => {
      setDisplayName('');
      setEmail('');
      setServerId('');
      setNotes('');
      setIsCreateOpen(false);
      const nextParams = new URLSearchParams(searchParams);
      nextParams.delete('create');
      nextParams.delete('page');
      setSearchParams(nextParams, { replace: true });
      await queryClient.invalidateQueries({ queryKey: ['vpn-accounts'] });
      const query = nextParams.toString();
      navigate(`/vpn-accounts/${account.id}${query ? `?${query}` : ''}`);
    },
  });

  const canCreateAccount = displayName.trim() !== '';

  function openCreateForm() {
    setIsCreateOpen(true);
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      next.set('create', '1');
      return next;
    }, { replace: true });
  }

  function closeCreateForm() {
    setIsCreateOpen(false);
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      next.delete('create');
      return next;
    }, { replace: true });
  }

  function handleCreateAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (canCreateAccount) createAccountMutation.mutate();
  }

  const credentials = credentialsQuery.data;

  return (
    <section className="page vpn-accounts-page feature-screen-page vpn-account-management-page" style={{ overflowX: 'hidden' }}>
      <div className="page-header feature-page-header">
        <div>
          <h1>{t('vpnAccounts.title')}</h1>
          <p>{t('vpnAccounts.subtitle')}</p>
        </div>
      </div>

      {isCreateOpen && (
        <div className="panel vpn-account-create-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">{t('vpnAccounts.createAction')}</div>
              <p className="panel-subtitle">{t('vpnAccounts.emptyDescription')}</p>
            </div>
          </div>
          <form className="vpn-account-create-form" onSubmit={handleCreateAccount}>
            <div className="vpn-account-create-grid">
              <label className="field">
                <span>{copy.accountName}</span>
                <input
                  value={displayName}
                  onChange={(event) => setDisplayName(event.target.value)}
                  placeholder={copy.accountNamePlaceholder}
                />
                <small>{copy.accountNameHint}</small>
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
                    <option value={server.id} key={server.id}>{server.name || server.id}</option>
                  ))}
                </select>
              </label>
              <label className="field vpn-account-notes-field">
                <span>{copy.notes}</span>
                <textarea
                  value={notes}
                  maxLength={4000}
                  rows={4}
                  placeholder={copy.notesPlaceholder}
                  onChange={(event) => setNotes(event.target.value)}
                />
                <small>{copy.notesHint}</small>
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
              <button className="small-button" type="button" onClick={closeCreateForm}>{t('common.cancel')}</button>
            </div>
          </form>
        </div>
      )}

      <div className="vpn-account-management-layout">
        <VpnAccountManagementList onCreate={openCreateForm} />

        <div className="vpn-account-management-detail-stack">
          <VpnAccountManagementPanel accountId={accountId} />

          {accountId && (
            <div className="panel credentials-panel feature-detail-panel">
              <div className="panel-header">
                <div>
                  <div className="panel-title">{t('vpnAccounts.credentialsTitle')}</div>
                  <p className="panel-subtitle">{t('vpnAccounts.credentialsSubtitle')}</p>
                </div>
              </div>
              {credentialsQuery.isLoading && <p className="empty-state">{t('vpnAccounts.loadingCredentials')}</p>}
              {credentialsQuery.isError && <div className="form-message form-message-error">{t('vpnAccounts.credentialsLoadError')}</div>}
              {credentials && (
                <div className="detail-list credentials-detail-list feature-detail-list">
                  <DetailRow label={t('vpnAccounts.accountId')}>{formatValue(credentials.vpnAccountId)}</DetailRow>
                  <DetailRow label={t('vpnAccounts.serverId')}>{formatValue(credentials.serverId)}</DetailRow>
                  <DetailRow label={t('vpnAccounts.protocol')}>{formatValue(credentials.protocol)}</DetailRow>
                  <DetailRow label={t('vpnAccounts.endpoint')}>{formatValue(credentials.endpoint)}</DetailRow>
                  <DetailRow label={t('vpnAccounts.vlessUuid')}><code>{formatValue(credentials.vless.uuid)}</code></DetailRow>
                  <DetailRow label={t('vpnAccounts.flow')}>{formatValue(credentials.vless.flow)}</DetailRow>
                  <DetailRow label={t('vpnAccounts.network')}>{formatValue(credentials.vless.network)}</DetailRow>
                  <DetailRow label={t('vpnAccounts.realityEnabled')}>{credentials.reality.enabled ? t('vpnAccounts.enabled') : t('vpnAccounts.disabled')}</DetailRow>
                  <DetailRow label={t('vpnAccounts.realityPublicKey')}><code>{formatValue(credentials.reality.publicKey)}</code></DetailRow>
                  <DetailRow label={t('vpnAccounts.realityShortId')}><code>{formatValue(credentials.reality.shortId)}</code></DetailRow>
                  <DetailRow label={t('vpnAccounts.realityServerName')}>{formatValue(credentials.reality.serverName)}</DetailRow>
                </div>
              )}
            </div>
          )}

          {accountId && <TrafficStatsPanel accountId={accountId} />}
          {accountId && <VpnClientConnectionPanel accountId={accountId} />}
        </div>
      </div>
    </section>
  );
}
