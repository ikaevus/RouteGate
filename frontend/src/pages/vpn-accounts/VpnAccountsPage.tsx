import { type FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { getServers } from '../../entities/server/api/serverApi';
import { createVpnAccount } from '../../entities/vpnAccount/api/vpnAccountApi';
import { t } from '../../shared/i18n/i18n';
import { AccessDevicesPanel } from './AccessDevicesPanel';
import { TrafficStatsPanel } from './TrafficStatsPanel';
import { VpnAccountConnectionPanels } from './VpnAccountConnectionPanels';
import { VpnAccountManagementList } from './VpnAccountManagementList';
import { VpnAccountManagementPanel } from './VpnAccountManagementPanel';
import { VpnAccountRoutingPolicyPanel } from './VpnAccountRoutingPolicyPanel';
import { getVpnAccountManagementCopy } from './vpnAccountManagementCopy';
import './vpnAccountManagement.css';
import './vpnAccountNotes.css';

function getErrorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() !== '' ? error.message : fallback;
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
  const [phone, setPhone] = useState('');
  const [telegramUsername, setTelegramUsername] = useState('');
  const [serverId, setServerId] = useState('');

  useEffect(() => {
    if (searchParams.get('create') === '1') setIsCreateOpen(true);
  }, [searchParams]);

  const serversQuery = useQuery({ queryKey: ['servers'], queryFn: getServers });

  const createAccountMutation = useMutation({
    mutationFn: () => createVpnAccount({
      displayName: displayName.trim(),
      email: email.trim() || undefined,
      phone: phone.trim() || undefined,
      telegramUsername: telegramUsername.trim() || undefined,
      serverId: serverId || undefined,
    }),
    onSuccess: async (account) => {
      setDisplayName('');
      setEmail('');
      setPhone('');
      setTelegramUsername('');
      setServerId('');
      setIsCreateOpen(false);
      const nextParams = new URLSearchParams(searchParams);
      nextParams.delete('create');
      nextParams.delete('page');
      nextParams.set('addDevice', '1');
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

  return (
    <section className="page vpn-accounts-page feature-screen-page vpn-account-management-page" style={{ overflowX: 'hidden' }}>
      <div className="page-header feature-page-header">
        <div>
          <h1>{t('vpnAccounts.title')}</h1>
          <p>{copy.pageSubtitle}</p>
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
                <span>{t('vpnAccounts.phone')}</span>
                <input
                  type="tel"
                  value={phone}
                  onChange={(event) => setPhone(event.target.value)}
                  placeholder={t('vpnAccounts.phonePlaceholder')}
                />
              </label>
              <label className="field">
                <span>{t('vpnAccounts.telegramUsername')}</span>
                <input
                  value={telegramUsername}
                  onChange={(event) => setTelegramUsername(event.target.value)}
                  placeholder={t('vpnAccounts.telegramUsernamePlaceholder')}
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
          {accountId && <AccessDevicesPanel accountId={accountId} />}
          {accountId && <VpnAccountRoutingPolicyPanel accountId={accountId} />}
          {accountId && <VpnAccountConnectionPanels accountId={accountId} />}
          {accountId && <TrafficStatsPanel accountId={accountId} />}

        </div>
      </div>
    </section>
  );
}
