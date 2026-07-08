import { type FormEvent, useEffect, useState, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { getServers } from '../../entities/server/api/serverApi';
import {
  createVpnAccount,
  createVpnAccountSubscriptionToken,
  getPublicSubscription,
  getVpnAccountCredentials,
  getVpnAccountSubscriptionQRCode,
  getVpnAccounts,
  rotateVpnAccountSubscriptionToken,
  type SubscriptionTokenResponse,
  type VpnAccount,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { SubscriptionQrDialog } from '../../shared/ui/SubscriptionQrDialog';
import { t } from '../../shared/i18n/i18n';
import { EmptyState } from '../../shared/ui/EmptyState';
import { StatusBadge } from '../../shared/ui/StatusBadge';
import { TrafficStatsPanel } from './TrafficStatsPanel';

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
  const [subscriptionToken, setSubscriptionToken] = useState<SubscriptionTokenResponse | null>(null);
  const [copiedTarget, setCopiedTarget] = useState<string | null>(null);
  const [isCreateOpen, setIsCreateOpen] = useState(searchParams.get('create') === '1');
  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');
  const [serverId, setServerId] = useState('');
  const [isQrOpen, setIsQrOpen] = useState(false);

  useEffect(() => {
    setSubscriptionToken(null);
    setCopiedTarget(null);
  }, [accountId]);

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

  const qrQuery = useQuery({
    queryKey: ['vpn-account-subscription-qr', accountId, subscriptionToken?.subscriptionToken],
    queryFn: () => getVpnAccountSubscriptionQRCode(accountId ?? '', subscriptionToken?.subscriptionToken ?? ''),
    enabled: Boolean(accountId && subscriptionToken?.subscriptionToken),
  });

  const publicSubscriptionQuery = useQuery({
    queryKey: ['public-subscription-preview', subscriptionToken?.subscriptionToken],
    queryFn: () => getPublicSubscription(subscriptionToken?.subscriptionToken ?? ''),
    enabled: Boolean(subscriptionToken?.subscriptionToken),
  });

  const createSubscriptionTokenMutation = useMutation({
    mutationFn: () => createVpnAccountSubscriptionToken(accountId ?? ''),
    onMutate: () => {
      setSubscriptionToken(null);
      setCopiedTarget(null);
    },
    onSuccess: (response) => {
      setSubscriptionToken(response);
    },
  });

  const rotateSubscriptionTokenMutation = useMutation({
    mutationFn: () => rotateVpnAccountSubscriptionToken(accountId ?? ''),
    onMutate: () => {
      setSubscriptionToken(null);
      setCopiedTarget(null);
    },
    onSuccess: (response) => {
      setSubscriptionToken(response);
    },
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

  const copyToClipboard = async (target: string, value: string) => {
    if (!navigator.clipboard) {
      return;
    }

    await navigator.clipboard.writeText(value);
    setCopiedTarget(target);
    window.setTimeout(() => setCopiedTarget(null), 1800);
  };

  const accounts = accountsQuery.data?.items ?? [];
  const selectedAccount = accounts.find((account) => account.id === accountId);
  const credentials = credentialsQuery.data;
  const qr = qrQuery.data;
  const publicSubscription = publicSubscriptionQuery.data;
  const renderedConfig = publicSubscription?.config.rendered;
  const renderedConfigText = renderedConfig
    ? JSON.stringify(renderedConfig.content, null, 2)
    : '';
  const isSubscriptionBusy =
    createSubscriptionTokenMutation.isPending || rotateSubscriptionTokenMutation.isPending;
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

        {accountId && selectedAccount && (
          <div className="panel subscription-panel feature-detail-panel">
            <div className="panel-header">
              <div>
                <div className="panel-title">{t('vpnAccounts.subscriptionTitle')}</div>
                <p className="panel-subtitle">{t('vpnAccounts.subscriptionSubtitle')}</p>
              </div>
              <div className="table-actions">
                <button
                  className="small-button"
                  type="button"
                  disabled={isSubscriptionBusy}
                  onClick={() => createSubscriptionTokenMutation.mutate()}
                >
                  {createSubscriptionTokenMutation.isPending ? t('vpnAccounts.generating') : t('vpnAccounts.generateSubscription')}
                </button>
                <button
                  className="small-button"
                  type="button"
                  disabled={isSubscriptionBusy}
                  onClick={() => rotateSubscriptionTokenMutation.mutate()}
                >
                  {rotateSubscriptionTokenMutation.isPending ? t('vpnAccounts.rotating') : t('vpnAccounts.rotateToken')}
                </button>
              </div>
            </div>

            {(createSubscriptionTokenMutation.isError || rotateSubscriptionTokenMutation.isError) && (
              <div className="form-message form-message-error">{t('vpnAccounts.subscriptionCreateError')}</div>
            )}

            {!subscriptionToken && (
            <EmptyState
                title={t('vpnAccounts.noSubscriptionTitle')}
                description={t('vpnAccounts.noSubscriptionDescription')}
              />
            )}

            {subscriptionToken && (
              <div className="subscription-result subscription-self-service-card" style={{ minWidth: 0 }}>
                <div className="form-message form-message-warning">
                  {t('vpnAccounts.subscriptionWarning')}
                </div>

                <div className="subscription-url-stack">
                  <div className="subscription-url-header">
                    <div className="subscription-url-meta">
                      <div className="subscription-url-label">{t('vpnAccounts.subscriptionUrl')}</div>
                      <p className="subscription-url-helper">{t('vpnAccounts.subscriptionUrlHelper')}</p>
                    </div>
                    <div className="table-actions">
                      <button
                        className="small-button"
                        type="button"
                        onClick={() => void copyToClipboard('subscription-url', subscriptionToken.subscriptionUrl)}
                      >
                        {copiedTarget === 'subscription-url' ? t('vpnAccounts.copied') : t('vpnAccounts.copy')}
                      </button>
                      <button
                        className="small-button"
                        type="button"
                        onClick={() => setIsQrOpen(true)}
                        disabled={!qr?.qrText}
                      >
                        {t('vpnAccounts.showQr')}
                      </button>
                    </div>
                  </div>
                  <code className="subscription-url-value">{subscriptionToken.subscriptionUrl}</code>
                </div>

                <div className="subscription-secondary-meta">
                  <div className="subscription-meta-chip">
                    <span>{t('vpnAccounts.expiresAt')}</span>
                    <strong>{formatDate(subscriptionToken.expiresAt)}</strong>
                  </div>
                  <div className="subscription-meta-chip">
                    <span>{t('vpnAccounts.subscriptionToken')}</span>
                    <strong>{formatValue(subscriptionToken.subscriptionToken)}</strong>
                  </div>
                </div>

                {qrQuery.isLoading && <p className="empty-state">{t('vpnAccounts.loadingQrPayload')}</p>}
                {qrQuery.isError && (
                  <div className="form-message form-message-error">{t('vpnAccounts.qrPayloadLoadError')}</div>
                )}

                <div className="client-config-preview feature-subpanel">
                  <div className="panel-header client-config-header">
                    <div>
                      <div className="panel-title token-snippet-title">{t('vpnAccounts.clientConfigPreview')}</div>
                      <p className="panel-subtitle">{t('vpnAccounts.clientConfigSubtitle')}</p>
                    </div>
                    {renderedConfig && (
                      <button
                        className="small-button"
                        type="button"
                        onClick={() => void copyToClipboard('client-config', renderedConfigText)}
                      >
                        {copiedTarget === 'client-config' ? t('vpnAccounts.copied') : t('vpnAccounts.copyConfig')}
                      </button>
                    )}
                  </div>

                  {publicSubscriptionQuery.isLoading && (
                    <p className="empty-state">{t('vpnAccounts.loadingClientConfig')}</p>
                  )}

                  {publicSubscriptionQuery.isError && (
                    <div className="form-message form-message-error">{t('vpnAccounts.publicSubscriptionLoadError')}</div>
                  )}

                  {publicSubscription && (
                    <div className="subscription-meta-grid">
                      <DetailRow label={t('vpnAccounts.subscriptionFormat')}>{formatValue(publicSubscription.format)}</DetailRow>
                      <DetailRow label={t('vpnAccounts.configStatus')}><StatusBadge status={publicSubscription.config.status} /></DetailRow>
                      <DetailRow label={t('vpnAccounts.configFormat')}>{formatValue(renderedConfig?.format ?? publicSubscription.config.format)}</DetailRow>
                      <DetailRow label={t('vpnAccounts.generatedAt')}>{formatDate(publicSubscription.generatedAt)}</DetailRow>
                      <DetailRow label={t('vpnAccounts.serverEndpoint')}>{formatValue(publicSubscription.server?.endpoint)}</DetailRow>
                    </div>
                  )}

                  {publicSubscription?.config.message && (
                    <div className="form-message form-message-warning">{publicSubscription.config.message}</div>
                  )}

                  {renderedConfig ? (
                    <pre className="code-block client-config-code" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxWidth: '100%' }}>{renderedConfigText}</pre>
                  ) : publicSubscription && (
                    <p className="empty-state">{t('vpnAccounts.noRenderedConfig')}</p>
                  )}
                </div>
              </div>
            )}

            <SubscriptionQrDialog
              isOpen={isQrOpen}
              title={t('vpnAccounts.subscriptionQrCode')}
              onClose={() => setIsQrOpen(false)}
              qrText={qr?.qrText}
              qrTitle={t('vpnAccounts.subscriptionQrCode')}
              qrSubtitle={t('vpnAccounts.format', { format: formatValue(qr?.format) })}
              url={subscriptionToken?.subscriptionUrl}
              urlLabel={t('vpnAccounts.subscriptionUrl')}
              onCopyQrText={() => void copyToClipboard('qr-text', qr?.qrText ?? '')}
              copyQrLabel={t('vpnAccounts.copyQrText')}
              copyCopiedLabel={t('vpnAccounts.copied')}
              copied={copiedTarget === 'qr-text'}
              closeLabel={t('common.close')}
              loadingLabel={t('vpnAccounts.qrPayloadLoadError')}
              unavailableLabel={t('vpnAccounts.qrPayloadLoadError')}
            />
          </div>
        )}
      </div>
    </section>
  );
}
