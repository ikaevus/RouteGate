import { useEffect, useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import {
  createVpnAccountSubscriptionToken,
  getPublicSubscription,
  getVpnAccountCredentials,
  getVpnAccountSubscriptionQRCode,
  getVpnAccounts,
  rotateVpnAccountSubscriptionToken,
  type SubscriptionTokenResponse,
  type VpnAccount,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { ScannableQrCode } from '../../shared/qr/ScannableQrCode';
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
      <div>
        <strong>{formatValue(account.displayName)}</strong>
        <span>{formatValue(account.email)}</span>
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
  const [subscriptionToken, setSubscriptionToken] = useState<SubscriptionTokenResponse | null>(null);
  const [copiedTarget, setCopiedTarget] = useState<string | null>(null);

  useEffect(() => {
    setSubscriptionToken(null);
    setCopiedTarget(null);
  }, [accountId]);

  const accountsQuery = useQuery({
    queryKey: ['vpn-accounts'],
    queryFn: getVpnAccounts,
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

  return (
    <section className="page vpn-accounts-page feature-screen-page">
      <div className="page-header feature-page-header">
        <div>
          <h1>VPN Accounts</h1>
          <p>View VLESS / Reality credentials, subscription URLs, client config previews, and traffic limits.</p>
        </div>

        <div className="status-pill">
          <span className="status-dot status-dot-ok" />
          {accounts.length} accounts
        </div>
      </div>

      <div className="vpn-accounts-layout feature-screen-layout">
        <div className="panel admin-table-panel feature-list-panel">
          <div className="panel-title">Accounts</div>

          {accountsQuery.isLoading && <p className="empty-state">Loading VPN accounts...</p>}

          {accountsQuery.isError && (
            <div className="form-message form-message-error">Failed to load VPN accounts.</div>
          )}

          {accountsQuery.isSuccess && accounts.length === 0 && (
            <EmptyState
              title="No VPN accounts yet"
              description="Create a VPN account to issue credentials, subscription links, and client configuration to a user."
            />
          )}

          {accounts.length > 0 && (
            <div className="admin-table vpn-accounts-table">
              <div className="admin-table-row admin-table-head vpn-accounts-table-row">
                <span>Account</span>
                <span>Status</span>
                <span>Server</span>
                <span>VLESS UUID</span>
                <span>Expires</span>
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
              <div className="panel-title">VLESS / Reality credentials</div>
              <p className="panel-subtitle">
                Client-facing values for the selected account. Private Reality keys are not shown.
              </p>
            </div>
          </div>

          {!accountId && (
            <EmptyState
              title="Select a VPN account"
              description="Choose an account from the list to view credentials, subscription details, and traffic policy."
            />
          )}

          {accountId && accountsQuery.isSuccess && !selectedAccount && (
            <div className="form-message form-message-error">Selected VPN account is not in the current list.</div>
          )}

          {credentialsQuery.isLoading && (
            <p className="empty-state">Loading credentials...</p>
          )}

          {credentialsQuery.isError && (
            <div className="form-message form-message-error">Failed to load account credentials.</div>
          )}

          {credentials && (
            <div className="detail-list credentials-detail-list feature-detail-list">
              <DetailRow label="VPN account ID">{formatValue(credentials.vpnAccountId)}</DetailRow>
              <DetailRow label="Server ID">{formatValue(credentials.serverId)}</DetailRow>
              <DetailRow label="Protocol">{formatValue(credentials.protocol)}</DetailRow>
              <DetailRow label="Endpoint">{formatValue(credentials.endpoint)}</DetailRow>
              <DetailRow label="VLESS UUID">
                <code>{formatValue(credentials.vless.uuid)}</code>
              </DetailRow>
              <DetailRow label="Flow">{formatValue(credentials.vless.flow)}</DetailRow>
              <DetailRow label="Network">{formatValue(credentials.vless.network)}</DetailRow>
              <DetailRow label="Reality enabled">
                {credentials.reality.enabled ? 'enabled' : 'disabled'}
              </DetailRow>
              <DetailRow label="Reality public key">
                <code>{formatValue(credentials.reality.publicKey)}</code>
              </DetailRow>
              <DetailRow label="Reality short ID">
                <code>{formatValue(credentials.reality.shortId)}</code>
              </DetailRow>
              <DetailRow label="Reality server name">
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
                <div className="panel-title">Subscription and client config</div>
                <p className="panel-subtitle">
                  Generate a one-time visible subscription token and preview the rendered client payload.
                </p>
              </div>
              <div className="table-actions">
                <button
                  className="small-button"
                  type="button"
                  disabled={isSubscriptionBusy}
                  onClick={() => createSubscriptionTokenMutation.mutate()}
                >
                  {createSubscriptionTokenMutation.isPending ? 'Generating...' : 'Generate subscription'}
                </button>
                <button
                  className="small-button"
                  type="button"
                  disabled={isSubscriptionBusy}
                  onClick={() => rotateSubscriptionTokenMutation.mutate()}
                >
                  {rotateSubscriptionTokenMutation.isPending ? 'Rotating...' : 'Rotate token'}
                </button>
              </div>
            </div>

            {(createSubscriptionTokenMutation.isError || rotateSubscriptionTokenMutation.isError) && (
              <div className="form-message form-message-error">Failed to create subscription token.</div>
            )}

            {!subscriptionToken && (
              <EmptyState
                title="No subscription token generated"
                description="Generate a subscription to show URL, QR payload, and client config preview."
              />
            )}

            {subscriptionToken && (
              <div className="subscription-result">
                <div className="form-message form-message-warning">
                  Save this subscription URL now. The raw token is shown only once and is not stored by the frontend.
                </div>

                <div className="detail-list credentials-detail-list feature-detail-list">
                  <DetailRow label="Subscription token">
                    <code>{subscriptionToken.subscriptionToken}</code>
                  </DetailRow>
                  <DetailRow label="Subscription URL">
                    <span className="copyable-value">
                      <code>{subscriptionToken.subscriptionUrl}</code>
                      <button
                        className="small-button"
                        type="button"
                        onClick={() => void copyToClipboard('subscription-url', subscriptionToken.subscriptionUrl)}
                      >
                        {copiedTarget === 'subscription-url' ? 'Copied' : 'Copy'}
                      </button>
                    </span>
                  </DetailRow>
                  <DetailRow label="Expires at">{formatDate(subscriptionToken.expiresAt)}</DetailRow>
                </div>

                {qrQuery.isLoading && <p className="empty-state">Loading QR payload...</p>}
                {qrQuery.isError && (
                  <div className="form-message form-message-error">Failed to load subscription QR payload.</div>
                )}
                {qr && (
                  <div className="qr-payload-panel feature-subpanel">
                    <ScannableQrCode
                      value={qr.qrText}
                      title="Subscription QR code"
                      subtitle={`Format: ${formatValue(qr.format)}`}
                    />
                    <div>
                      <div className="panel-title token-snippet-title">Subscription QR payload</div>
                      <p className="panel-subtitle">Copyable source text used for QR rendering.</p>
                    </div>
                    <pre className="code-block">{qr.qrText}</pre>
                    <button
                      className="small-button"
                      type="button"
                      onClick={() => void copyToClipboard('qr-text', qr.qrText)}
                    >
                      {copiedTarget === 'qr-text' ? 'Copied' : 'Copy QR text'}
                    </button>
                  </div>
                )}

                <div className="client-config-preview feature-subpanel">
                  <div className="panel-header client-config-header">
                    <div>
                      <div className="panel-title token-snippet-title">Client config preview</div>
                      <p className="panel-subtitle">
                        Public subscription response and rendered sing-box config for this account.
                      </p>
                    </div>
                    {renderedConfig && (
                      <button
                        className="small-button"
                        type="button"
                        onClick={() => void copyToClipboard('client-config', renderedConfigText)}
                      >
                        {copiedTarget === 'client-config' ? 'Copied' : 'Copy config'}
                      </button>
                    )}
                  </div>

                  {publicSubscriptionQuery.isLoading && (
                    <p className="empty-state">Loading client config preview...</p>
                  )}

                  {publicSubscriptionQuery.isError && (
                    <div className="form-message form-message-error">Failed to load public subscription preview.</div>
                  )}

                  {publicSubscription && (
                    <div className="subscription-meta-grid">
                      <DetailRow label="Subscription format">{formatValue(publicSubscription.format)}</DetailRow>
                      <DetailRow label="Config status"><StatusBadge status={publicSubscription.config.status} /></DetailRow>
                      <DetailRow label="Config format">{formatValue(renderedConfig?.format ?? publicSubscription.config.format)}</DetailRow>
                      <DetailRow label="Generated at">{formatDate(publicSubscription.generatedAt)}</DetailRow>
                      <DetailRow label="Server endpoint">{formatValue(publicSubscription.server?.endpoint)}</DetailRow>
                    </div>
                  )}

                  {publicSubscription?.config.message && (
                    <div className="form-message form-message-warning">{publicSubscription.config.message}</div>
                  )}

                  {renderedConfig ? (
                    <pre className="code-block client-config-code">{renderedConfigText}</pre>
                  ) : publicSubscription && (
                    <p className="empty-state">No rendered client config is available for this subscription yet.</p>
                  )}
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </section>
  );
}
