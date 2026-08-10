import { type FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { getServers } from '../../entities/server/api/serverApi';
import {
  activateVpnAccountManagement,
  deleteVpnAccount,
  getVpnAccount,
  getVpnAccountNotes,
  revokeVpnAccount,
  suspendVpnAccount,
  updateVpnAccount,
  updateVpnAccountNotes,
  type VpnAccountStatus,
} from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { t } from '../../shared/i18n/i18n';
import { EmptyState } from '../../shared/ui/EmptyState';
import { StatusBadge } from '../../shared/ui/StatusBadge';
import { getVpnAccountManagementCopy } from './vpnAccountManagementCopy';

export function VpnAccountManagementPanel({ accountId }: { accountId?: string }) {
  const copy = getVpnAccountManagementCopy();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();

  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');
  const [status, setStatus] = useState<VpnAccountStatus>('active');
  const [serverId, setServerId] = useState('');
  const [notes, setNotes] = useState('');
  const [message, setMessage] = useState('');
  const [errorMessage, setErrorMessage] = useState('');
  const [configurationChanged, setConfigurationChanged] = useState(false);

  const accountQuery = useQuery({
    queryKey: ['vpn-account', accountId],
    queryFn: () => getVpnAccount(accountId ?? ''),
    enabled: Boolean(accountId),
  });

  const notesQuery = useQuery({
    queryKey: ['vpn-account-notes', accountId],
    queryFn: () => getVpnAccountNotes(accountId ?? ''),
    enabled: Boolean(accountId),
  });

  const serversQuery = useQuery({ queryKey: ['servers'], queryFn: getServers });

  useEffect(() => {
    const account = accountQuery.data;
    if (!account) return;
    setDisplayName(account.displayName);
    setEmail(account.email ?? '');
    setStatus(account.status as VpnAccountStatus);
    setServerId(account.serverId ?? '');
    setMessage('');
    setErrorMessage('');
    setConfigurationChanged(false);
  }, [accountQuery.data]);

  useEffect(() => {
    if (!notesQuery.data) return;
    setNotes(notesQuery.data.notes ?? '');
  }, [notesQuery.data]);

  async function refreshAccountData() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['vpn-accounts'] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-account', accountId] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-account-notes', accountId] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-account-credentials', accountId] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-account-client-connection', accountId] }),
    ]);
  }

  const updateMutation = useMutation({
    mutationFn: async () => {
      const previous = accountQuery.data;
      if (!previous) throw new Error('VPN account is not loaded');

      const nextDisplayName = displayName.trim();
      const nextEmail = email.trim();
      const nextServerId = serverId;
      const previousServerId = previous.serverId ?? '';
      const previousNotes = notesQuery.data?.notes ?? '';

      const accountChanged = previous.displayName !== nextDisplayName
        || (previous.email ?? '') !== nextEmail
        || previous.status !== status
        || previousServerId !== nextServerId;
      const configFieldsChanged = previous.displayName !== nextDisplayName
        || previous.status !== status
        || previousServerId !== nextServerId;
      const affectsConfig = configFieldsChanged && Boolean(previousServerId || nextServerId);
      const notesChanged = previousNotes !== notes;

      const accountPromise = accountChanged
        ? updateVpnAccount(accountId ?? '', {
            displayName: nextDisplayName,
            email: nextEmail,
            status,
            serverId: nextServerId,
          })
        : Promise.resolve(previous);
      const notesPromise = notesChanged
        ? updateVpnAccountNotes(accountId ?? '', notes)
        : Promise.resolve(notesQuery.data);

      const [updated] = await Promise.all([accountPromise, notesPromise]);
      return { updated, affectsConfig };
    },
    onSuccess: async ({ updated, affectsConfig }) => {
      setStatus(updated.status as VpnAccountStatus);
      setMessage(copy.editSuccess);
      setErrorMessage('');
      setConfigurationChanged(affectsConfig);
      await refreshAccountData();
    },
    onError: () => {
      setMessage('');
      setErrorMessage(copy.editError);
    },
  });

  const statusMutation = useMutation({
    mutationFn: (nextStatus: 'active' | 'suspended' | 'revoked') => {
      if (!accountId) throw new Error('Missing VPN account ID');
      if (nextStatus === 'active') return activateVpnAccountManagement(accountId);
      if (nextStatus === 'suspended') return suspendVpnAccount(accountId);
      return revokeVpnAccount(accountId);
    },
    onSuccess: async (updated) => {
      setStatus(updated.status as VpnAccountStatus);
      setMessage(copy.editSuccess);
      setErrorMessage('');
      setConfigurationChanged(Boolean(updated.serverId));
      await refreshAccountData();
    },
    onError: () => {
      setMessage('');
      setErrorMessage(copy.editError);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => deleteVpnAccount(accountId ?? ''),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['vpn-accounts'] });
      const query = searchParams.toString();
      navigate(`/vpn-accounts${query ? `?${query}` : ''}`, { replace: true });
    },
    onError: () => setErrorMessage(copy.deleteError),
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!displayName.trim() || updateMutation.isPending) return;
    updateMutation.mutate();
  }

  function handleDelete() {
    const name = accountQuery.data?.displayName ?? accountId ?? '';
    if (window.confirm(copy.deleteConfirm(name))) deleteMutation.mutate();
  }

  if (!accountId) {
    return (
      <div className="panel feature-detail-panel vpn-account-management-panel">
        <EmptyState title={t('vpnAccounts.selectTitle')} description={t('vpnAccounts.selectDescription')} />
      </div>
    );
  }

  if (accountQuery.isLoading) {
    return <div className="panel feature-detail-panel vpn-account-management-panel"><p className="empty-state">{t('common.loading')}</p></div>;
  }

  if (accountQuery.isError || !accountQuery.data) {
    return <div className="panel feature-detail-panel vpn-account-management-panel"><div className="form-message form-message-error">{copy.loadAccountError}</div></div>;
  }

  const account = accountQuery.data;
  const actionPending = statusMutation.isPending || deleteMutation.isPending;

  return (
    <div className="panel feature-detail-panel vpn-account-management-panel">
      <div className="panel-header vpn-account-editor-header">
        <div>
          <div className="panel-title">{copy.editTitle}</div>
          <p className="panel-subtitle">{copy.editSubtitle}</p>
        </div>
        <StatusBadge status={account.status} />
      </div>

      <form className="vpn-account-edit-form" onSubmit={handleSubmit}>
        <div className="vpn-account-edit-grid">
          <label className="field">
            <span>{copy.accountName}</span>
            <input value={displayName} onChange={(event) => setDisplayName(event.target.value)} required />
            <small>{copy.accountNameHint}</small>
          </label>
          <label className="field">
            <span>{t('vpnAccounts.email')}</span>
            <input type="email" value={email} onChange={(event) => setEmail(event.target.value)} />
          </label>
          <label className="field">
            <span>{t('vpnAccounts.status')}</span>
            <select value={status} onChange={(event) => setStatus(event.target.value as VpnAccountStatus)}>
              <option value="active">{copy.statusActive}</option>
              <option value="created">{copy.statusCreated}</option>
              <option value="suspended">{copy.statusSuspended}</option>
              <option value="expired">{copy.statusExpired}</option>
              <option value="revoked">{copy.statusRevoked}</option>
            </select>
          </label>
          <label className="field">
            <span>{t('vpnAccounts.serverAssignment')}</span>
            <select value={serverId} onChange={(event) => setServerId(event.target.value)}>
              <option value="">{t('vpnAccounts.noServerAssignment')}</option>
              {(serversQuery.data?.items ?? []).map((server) => (
                <option key={server.id} value={server.id}>{server.name || server.id}</option>
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

        {notesQuery.isError && <div className="form-message form-message-error">{copy.notesLoadError}</div>}

        <div className="form-actions">
          <button className="primary-button" type="submit" disabled={!displayName.trim() || updateMutation.isPending || notesQuery.isLoading}>
            {updateMutation.isPending ? copy.saving : copy.save}
          </button>
        </div>
      </form>

      {message && <div className="form-message form-message-success">{message}</div>}
      {errorMessage && <div className="form-message form-message-error">{errorMessage}</div>}
      {configurationChanged && (
        <div className="form-message vpn-account-config-notice">
          <span>{copy.configNotice}</span>
          <Link className="text-link" to="/config-deploy">{copy.openDeploy}</Link>
        </div>
      )}

      <div className="vpn-account-access-actions">
        <div>
          <strong>{copy.accessActions}</strong>
          <p className="panel-subtitle">{account.serverId ? copy.configNotice : copy.unassignedConfigNotice}</p>
        </div>
        <div className="form-actions">
          <button className="small-button" type="button" disabled={actionPending || account.status === 'active'} onClick={() => statusMutation.mutate('active')}>{copy.activate}</button>
          <button className="small-button" type="button" disabled={actionPending || account.status === 'suspended'} onClick={() => statusMutation.mutate('suspended')}>{copy.suspend}</button>
          <button className="small-button" type="button" disabled={actionPending || account.status === 'revoked'} onClick={() => statusMutation.mutate('revoked')}>{copy.revoke}</button>
        </div>
      </div>

      <div className="vpn-account-danger-zone">
        <div>
          <strong>{copy.dangerZone}</strong>
          <p className="panel-subtitle">{account.id}</p>
        </div>
        <button className="danger-button" type="button" disabled={deleteMutation.isPending} onClick={handleDelete}>
          {copy.deleteAccount}
        </button>
      </div>
    </div>
  );
}
