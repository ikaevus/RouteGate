import { type FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  activateVpnAccountManagement,
  deleteVpnAccount,
  getVpnAccount,
  getVpnAccountNotes,
  revokeVpnAccount,
  suspendVpnAccount,
  updateVpnAccount,
  updateVpnAccountNotes,
} from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { t } from '../../shared/i18n/i18n';
import { CollapsiblePanelHeaderTitles } from '../../shared/ui/CollapsiblePanelHeader';
import { EmptyState } from '../../shared/ui/EmptyState';
import { StatusBadge } from '../../shared/ui/StatusBadge';
import { getVpnAccountManagementCopy } from './vpnAccountManagementCopy';

// Identity/administrative data only. Status changes go through the explicit
// Activate/Suspend/Revoke actions below (never a status dropdown), and server
// placement lives in Routing & Placement, not here.
export function VpnAccountManagementPanel({ accountId }: { accountId?: string }) {
  const copy = getVpnAccountManagementCopy();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();

  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');
  const [phone, setPhone] = useState('');
  const [telegramUsername, setTelegramUsername] = useState('');
  const [notes, setNotes] = useState('');
  const [message, setMessage] = useState('');
  const [errorMessage, setErrorMessage] = useState('');
  const [isOpen, setIsOpen] = useState(true);

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

  useEffect(() => {
    const account = accountQuery.data;
    if (!account) return;
    setDisplayName(account.displayName);
    setEmail(account.email ?? '');
    setPhone(account.phone ?? '');
    setTelegramUsername(account.telegramUsername ?? '');
    setMessage('');
    setErrorMessage('');
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
    ]);
  }

  const updateMutation = useMutation({
    mutationFn: async () => {
      const previous = accountQuery.data;
      if (!previous) throw new Error('VPN account is not loaded');

      const nextDisplayName = displayName.trim();
      const nextEmail = email.trim();
      const nextPhone = phone.trim();
      const nextTelegramUsername = telegramUsername.trim();
      const previousNotes = notesQuery.data?.notes ?? '';

      const accountChanged = previous.displayName !== nextDisplayName
        || (previous.email ?? '') !== nextEmail
        || (previous.phone ?? '') !== nextPhone
        || (previous.telegramUsername ?? '') !== nextTelegramUsername;
      const notesChanged = previousNotes !== notes;

      const accountPromise = accountChanged
        ? updateVpnAccount(accountId ?? '', {
            displayName: nextDisplayName,
            email: nextEmail,
            phone: nextPhone,
            telegramUsername: nextTelegramUsername,
          })
        : Promise.resolve(previous);
      const notesPromise = notesChanged
        ? updateVpnAccountNotes(accountId ?? '', notes)
        : Promise.resolve(notesQuery.data);

      await Promise.all([accountPromise, notesPromise]);
    },
    onSuccess: async () => {
      setMessage(copy.editSuccess);
      setErrorMessage('');
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
    onSuccess: async () => {
      setMessage(copy.editSuccess);
      setErrorMessage('');
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
        <CollapsiblePanelHeaderTitles
          title={copy.editTitle}
          subtitle={copy.editSubtitle}
          open={isOpen}
          onToggle={() => setIsOpen((value) => !value)}
        />
        <StatusBadge status={account.status} />
      </div>

      <div className="panel-collapsible-body" hidden={!isOpen}>
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
            <span>{t('vpnAccounts.phone')}</span>
            <input
              type="tel"
              value={phone}
              placeholder={t('vpnAccounts.phonePlaceholder')}
              onChange={(event) => setPhone(event.target.value)}
            />
            <small>{t('vpnAccounts.phoneHint')}</small>
          </label>
          <label className="field">
            <span>{t('vpnAccounts.telegramUsername')}</span>
            <input
              value={telegramUsername}
              placeholder={t('vpnAccounts.telegramUsernamePlaceholder')}
              onChange={(event) => setTelegramUsername(event.target.value)}
            />
            <small>{t('vpnAccounts.telegramUsernameHint')}</small>
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

      <div className="vpn-account-access-actions">
        <div>
          <strong>{copy.accessActions}</strong>
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
    </div>
  );
}
