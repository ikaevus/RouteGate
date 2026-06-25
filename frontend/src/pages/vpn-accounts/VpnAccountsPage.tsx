import { type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import {
  getVpnAccountCredentials,
  getVpnAccounts,
  type VpnAccount,
} from '../../entities/vpnAccount/api/vpnAccountApi';

function formatDate(value?: string | null): string {
  if (!value) {
    return '-';
  }

  const date = new Date(value);

  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatValue(value?: string | null): string {
  return value && value.trim() !== '' ? value : '-';
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

  const accountsQuery = useQuery({
    queryKey: ['vpn-accounts'],
    queryFn: getVpnAccounts,
  });

  const credentialsQuery = useQuery({
    queryKey: ['vpn-account-credentials', accountId],
    queryFn: () => getVpnAccountCredentials(accountId ?? ''),
    enabled: Boolean(accountId),
  });

  const accounts = accountsQuery.data?.items ?? [];
  const selectedAccount = accounts.find((account) => account.id === accountId);
  const credentials = credentialsQuery.data;

  return (
    <section className="page vpn-accounts-page">
      <div className="page-header">
        <div>
          <h1>VPN Accounts</h1>
          <p>View VLESS / Reality credentials for managed VPN accounts.</p>
        </div>

        <div className="status-pill">
          <span className="status-dot status-dot-ok" />
          {accounts.length} accounts
        </div>
      </div>

      <div className="vpn-accounts-layout">
        <div className="panel admin-table-panel">
          <div className="panel-title">Accounts</div>

          {accountsQuery.isLoading && <p className="empty-state">Loading VPN accounts...</p>}

          {accountsQuery.isError && (
            <div className="form-message form-message-error">Failed to load VPN accounts.</div>
          )}

          {accountsQuery.isSuccess && accounts.length === 0 && (
            <p className="empty-state">No VPN accounts created yet.</p>
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

        <div className="panel credentials-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">VLESS / Reality credentials</div>
              <p className="panel-subtitle">
                Client-facing values for the selected account. Private Reality keys are not shown.
              </p>
            </div>
          </div>

          {!accountId && (
            <p className="empty-state">Select a VPN account to view credentials.</p>
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
            <div className="detail-list credentials-detail-list">
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
      </div>
    </section>
  );
}
