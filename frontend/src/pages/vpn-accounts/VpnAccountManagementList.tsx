import { type FormEvent, useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { getServers, type Server } from '../../entities/server/api/serverApi';
import {
  getPagedVpnAccounts,
  runBulkVpnAccountAction,
  type BulkVpnAccountAction,
} from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { t } from '../../shared/i18n/i18n';
import { EmptyState } from '../../shared/ui/EmptyState';
import { StatusBadge } from '../../shared/ui/StatusBadge';
import { getVpnAccountManagementCopy } from './vpnAccountManagementCopy';

interface BulkRunInput {
  action: BulkVpnAccountAction;
  targetServerId?: string;
}

const cssCountryFlags = new Set(['de', 'nl', 'sg', 'us']);
const hostnameCountryHints = new Set([
  'at', 'au', 'ca', 'ch', 'cz', 'de', 'dk', 'ee', 'fi', 'fr', 'gb', 'jp', 'kr',
  'kz', 'lt', 'lv', 'nl', 'no', 'pl', 'ru', 'se', 'sg', 'us',
]);

const countryNameCodes: Record<string, string> = {
  australia: 'au',
  austria: 'at',
  canada: 'ca',
  czechia: 'cz',
  denmark: 'dk',
  estonia: 'ee',
  finland: 'fi',
  france: 'fr',
  germany: 'de',
  japan: 'jp',
  kazakhstan: 'kz',
  latvia: 'lv',
  lithuania: 'lt',
  netherlands: 'nl',
  norway: 'no',
  poland: 'pl',
  russia: 'ru',
  singapore: 'sg',
  sweden: 'se',
  switzerland: 'ch',
  'united kingdom': 'gb',
  'united states': 'us',
  usa: 'us',
};

function positiveNumber(value: string | null, fallback: number): number {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}

function pageNumbers(page: number, totalPages: number): number[] {
  if (totalPages <= 1) return totalPages === 1 ? [1] : [];
  const values = new Set<number>([1, totalPages]);
  for (let current = Math.max(1, page - 2); current <= Math.min(totalPages, page + 2); current += 1) {
    values.add(current);
  }
  return Array.from(values).sort((left, right) => left - right);
}

function displayServerName(serverId: string | null | undefined, servers: Array<{ id: string; name: string }>): string {
  if (!serverId) return t('common.notAvailable');
  return servers.find((server) => server.id === serverId)?.name || serverId;
}

function serverCountryCode(server?: Pick<Server, 'location' | 'name'> | null): string | null {
  const location = server?.location?.trim() ?? '';
  if (location) {
    const explicitCode = location.match(/(?:^|,\s*)([A-Za-z]{2})$/)?.[1];
    if (explicitCode) return explicitCode.toLowerCase();

    const normalizedLocation = location.toLowerCase();
    const namedCountry = Object.entries(countryNameCodes).find(([name]) => normalizedLocation.includes(name));
    if (namedCountry) return namedCountry[1];
  }

  const nameHint = server?.name?.trim().toLowerCase().match(/^([a-z]{2})[.-]/)?.[1];
  return nameHint && hostnameCountryHints.has(nameHint) ? nameHint : null;
}

function countryFlagEmoji(countryCode: string): string {
  return countryCode
    .toUpperCase()
    .split('')
    .map((character) => String.fromCodePoint(127397 + character.charCodeAt(0)))
    .join('');
}

function serverOptionLabel(server: Server): string {
  const name = server.name || server.id;
  const countryCode = serverCountryCode(server);
  return countryCode ? `${countryFlagEmoji(countryCode)} ${name}` : name;
}

export function VpnAccountManagementList({ onCreate }: { onCreate: () => void }) {
  const copy = getVpnAccountManagementCopy();
  const { accountId } = useParams<{ accountId: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();

  const search = searchParams.get('q') ?? '';
  const status = searchParams.get('status') ?? '';
  const serverId = searchParams.get('server') ?? '';
  const page = positiveNumber(searchParams.get('page'), 1);
  const pageSize = [25, 50, 100].includes(positiveNumber(searchParams.get('pageSize'), 50))
    ? positiveNumber(searchParams.get('pageSize'), 50)
    : 50;

  const [searchInput, setSearchInput] = useState(search);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [allMatching, setAllMatching] = useState(false);
  const [assignmentServerId, setAssignmentServerId] = useState('');
  const [operationMessage, setOperationMessage] = useState('');
  const [operationNeedsDeploy, setOperationNeedsDeploy] = useState(false);
  const [operationError, setOperationError] = useState('');

  useEffect(() => setSearchInput(search), [search]);

  const selectionKey = `${search}|${status}|${serverId}|${page}|${pageSize}`;
  useEffect(() => {
    setSelectedIds(new Set());
    setAllMatching(false);
    setOperationMessage('');
    setOperationNeedsDeploy(false);
    setOperationError('');
  }, [selectionKey]);

  const accountsQuery = useQuery({
    queryKey: ['vpn-accounts', 'management', search, status, serverId, page, pageSize],
    queryFn: () => getPagedVpnAccounts({ search, status, serverId, page, pageSize }),
    placeholderData: (previous) => previous,
  });

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
  });

  const servers = serversQuery.data?.items ?? [];
  const accounts = accountsQuery.data?.items ?? [];
  const total = accountsQuery.data?.total ?? 0;
  const totalPages = accountsQuery.data?.totalPages ?? 0;
  const effectivePage = accountsQuery.data?.page ?? page;
  const effectivePageSize = accountsQuery.data?.pageSize ?? pageSize;
  const selectedCount = allMatching ? total : selectedIds.size;
  const allPageSelected = accounts.length > 0 && accounts.every((account) => selectedIds.has(account.id));
  const hasFilters = search !== '' || status !== '' || serverId !== '';

  const visiblePages = useMemo(() => pageNumbers(effectivePage, totalPages), [effectivePage, totalPages]);

  function updateListParams(changes: Record<string, string | number | null>, resetPage = true) {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      Object.entries(changes).forEach(([key, value]) => {
        if (value === null || value === '') next.delete(key);
        else next.set(key, String(value));
      });
      if (resetPage && !Object.prototype.hasOwnProperty.call(changes, 'page')) next.delete('page');
      return next;
    }, { replace: true });
  }

  function handleSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    updateListParams({ q: searchInput.trim() });
  }

  function resetFilters() {
    setSearchInput('');
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      ['q', 'status', 'server', 'page'].forEach((key) => next.delete(key));
      return next;
    }, { replace: true });
  }

  function rowHref(id: string): string {
    const query = searchParams.toString();
    return `/vpn-accounts/${encodeURIComponent(id)}${query ? `?${query}` : ''}`;
  }

  function toggleAccount(id: string) {
    setAllMatching(false);
    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function togglePage() {
    setAllMatching(false);
    setSelectedIds((current) => {
      const next = new Set(current);
      if (allPageSelected) accounts.forEach((account) => next.delete(account.id));
      else accounts.forEach((account) => next.add(account.id));
      return next;
    });
  }

  function clearSelection() {
    setSelectedIds(new Set());
    setAllMatching(false);
  }

  const bulkMutation = useMutation({
    mutationFn: ({ action, targetServerId }: BulkRunInput) => runBulkVpnAccountAction({
      action,
      targetServerId,
      selection: allMatching
        ? { allMatching: true, search, status, serverId }
        : { ids: Array.from(selectedIds) },
    }),
    onSuccess: async (result, variables) => {
      const deletedSelectedAccount = variables.action === 'delete'
        && Boolean(accountId)
        && (allMatching || selectedIds.has(accountId ?? ''));
      clearSelection();
      setOperationError('');
      setOperationMessage(copy.bulkDone(result.affectedCount));
      setOperationNeedsDeploy(result.configurationChanged && result.affectedServerIds.length > 0);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['vpn-accounts'] }),
        queryClient.invalidateQueries({ queryKey: ['vpn-account'] }),
      ]);
      if (deletedSelectedAccount) {
        const query = searchParams.toString();
        navigate(`/vpn-accounts${query ? `?${query}` : ''}`, { replace: true });
      }
    },
    onError: () => {
      setOperationMessage('');
      setOperationNeedsDeploy(false);
      setOperationError(copy.bulkError);
    },
  });

  function runBulk(action: BulkVpnAccountAction, targetServerId?: string) {
    if (selectedCount === 0 || bulkMutation.isPending) return;
    if (action === 'delete' && !window.confirm(copy.bulkConfirmDelete(selectedCount))) return;
    if (action === 'revoke' && !window.confirm(copy.bulkConfirmRevoke(selectedCount))) return;
    bulkMutation.mutate({ action, targetServerId });
  }

  const from = total === 0 ? 0 : (effectivePage - 1) * effectivePageSize + 1;
  const to = total === 0 ? 0 : Math.min(total, from + accounts.length - 1);

  return (
    <div className="panel admin-table-panel feature-list-panel vpn-account-management-list">
      <div className="panel-header vpn-account-list-heading">
        <div>
          <div className="panel-title">{t('vpnAccounts.accountsPanelTitle')}</div>
          <p className="panel-subtitle">{copy.shown(from, to, total)}</p>
        </div>
        <button className="small-button" type="button" onClick={onCreate}>
          {t('vpnAccounts.createAction')}
        </button>
      </div>

      <form className="vpn-account-management-toolbar" onSubmit={handleSearch} aria-label={copy.filtersLabel}>
        <div className="vpn-account-search-row">
          <input
            className="vpn-account-search-input"
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            placeholder={copy.searchPlaceholder}
          />
          <button className="small-button" type="submit">{copy.search}</button>
          {hasFilters && <button className="small-button" type="button" onClick={resetFilters}>{copy.clear}</button>}
        </div>
        <div className="vpn-account-filter-row">
          <select value={status} onChange={(event) => updateListParams({ status: event.target.value })}>
            <option value="">{copy.allStatuses}</option>
            <option value="active">{copy.statusActive}</option>
            <option value="created">{copy.statusCreated}</option>
            <option value="suspended">{copy.statusSuspended}</option>
            <option value="expired">{copy.statusExpired}</option>
            <option value="revoked">{copy.statusRevoked}</option>
          </select>
          <select value={serverId} onChange={(event) => updateListParams({ server: event.target.value })}>
            <option value="">{copy.allServers}</option>
            {servers.map((server) => <option key={server.id} value={server.id}>{serverOptionLabel(server)}</option>)}
          </select>
        </div>
      </form>

      {selectedCount > 0 && (
        <div className="vpn-account-bulk-toolbar">
          <div className="vpn-account-bulk-summary">
            <strong>{allMatching ? copy.allMatchingSelected(selectedCount) : copy.selected(selectedCount)}</strong>
            {!allMatching && allPageSelected && total > selectedIds.size && (
              <button className="text-button" type="button" onClick={() => setAllMatching(true)}>
                {copy.selectAllMatching(total)}
              </button>
            )}
            <button className="text-button" type="button" onClick={clearSelection}>{copy.clearSelection}</button>
          </div>
          <div className="vpn-account-bulk-actions">
            <button className="small-button" type="button" disabled={bulkMutation.isPending} onClick={() => runBulk('activate')}>{copy.activate}</button>
            <button className="small-button" type="button" disabled={bulkMutation.isPending} onClick={() => runBulk('suspend')}>{copy.suspend}</button>
            <button className="small-button" type="button" disabled={bulkMutation.isPending} onClick={() => runBulk('revoke')}>{copy.revoke}</button>
            <div className="vpn-account-bulk-assign">
              <select value={assignmentServerId} onChange={(event) => setAssignmentServerId(event.target.value)}>
                <option value="">{copy.assignServer}</option>
                {servers.map((server) => <option key={server.id} value={server.id}>{serverOptionLabel(server)}</option>)}
              </select>
              <button
                className="small-button"
                type="button"
                disabled={bulkMutation.isPending || !assignmentServerId}
                onClick={() => runBulk('assign_server', assignmentServerId)}
              >
                {copy.applyAssignment}
              </button>
            </div>
            <button className="danger-button" type="button" disabled={bulkMutation.isPending} onClick={() => runBulk('delete')}>{copy.delete}</button>
          </div>
          {bulkMutation.isPending && <span className="panel-subtitle">{copy.bulkWorking}</span>}
        </div>
      )}

      {operationMessage && (
        <div className={`form-message form-message-success${operationNeedsDeploy ? ' vpn-account-config-notice' : ''}`}>
          <span>{operationMessage}</span>
          {operationNeedsDeploy && (
            <>
              <span>{copy.configNotice}</span>
              <Link className="text-link" to="/config-deploy">{copy.openDeploy}</Link>
            </>
          )}
        </div>
      )}
      {operationError && <div className="form-message form-message-error">{operationError}</div>}

      {accountsQuery.isLoading && <p className="empty-state">{t('common.loading')}</p>}
      {accountsQuery.isError && <div className="form-message form-message-error">{t('vpnAccounts.loadError')}</div>}

      {accountsQuery.isSuccess && accounts.length === 0 && (
        <EmptyState
          title={hasFilters ? copy.noMatchesTitle : t('vpnAccounts.emptyTitle')}
          description={hasFilters ? copy.noMatchesDescription : t('vpnAccounts.emptyDescription')}
        />
      )}

      {accounts.length > 0 && (
        <div className="vpn-account-management-table" role="table">
          <div className="vpn-account-management-head" role="row">
            <input type="checkbox" checked={allPageSelected} onChange={togglePage} aria-label={copy.selectPage} />
            <span>{t('vpnAccounts.account')}</span>
            <span>{t('vpnAccounts.status')}</span>
            <span>{t('vpnAccounts.server')}</span>
            <span>{t('vpnAccounts.expires')}</span>
          </div>
          {accounts.map((account) => {
            const server = servers.find((candidate) => candidate.id === account.serverId);
            const countryCode = serverCountryCode(server);
            const useCssFlag = Boolean(countryCode && cssCountryFlags.has(countryCode));

            return (
              <div className={`vpn-account-management-row${account.id === accountId ? ' is-selected' : ''}`} role="row" key={account.id}>
                <input
                  type="checkbox"
                  checked={allMatching || selectedIds.has(account.id)}
                  onChange={() => toggleAccount(account.id)}
                  aria-label={account.displayName}
                />
                <Link className="vpn-account-management-row-link" to={rowHref(account.id)}>
                  <div className="portal-profile-cell">
                    <strong className="portal-profile-name">{account.displayName}</strong>
                    <span className="portal-profile-meta">{account.email || account.vlessUuid || account.id}</span>
                  </div>
                  <StatusBadge status={account.status} />
                  <span className="vpn-account-server-cell" title={server?.location || server?.name || account.serverId || ''}>
                    {countryCode && (
                      <span
                        className={`server-country-flag${useCssFlag ? ` server-country-${countryCode}` : ' server-country-emoji'}`}
                        aria-label={countryCode.toUpperCase()}
                      >
                        {useCssFlag ? '' : countryFlagEmoji(countryCode)}
                      </span>
                    )}
                    <span className="vpn-account-server-name">{displayServerName(account.serverId, servers)}</span>
                  </span>
                  <span>{account.expiresAt ? new Date(account.expiresAt).toLocaleDateString() : t('common.notAvailable')}</span>
                </Link>
              </div>
            );
          })}
        </div>
      )}

      {accounts.length > 0 && (
        <div className="vpn-account-pagination">
          <span className="panel-subtitle">{copy.shown(from, to, total)}</span>
          <div className="vpn-account-page-buttons">
            <button className="small-button" type="button" disabled={effectivePage <= 1} onClick={() => updateListParams({ page: effectivePage - 1 }, false)}>{copy.previous}</button>
            {visiblePages.map((pageNumber, index) => {
              const previous = visiblePages[index - 1];
              return (
                <span className="vpn-account-page-number-wrap" key={pageNumber}>
                  {previous && pageNumber - previous > 1 && <span className="vpn-account-page-ellipsis">…</span>}
                  <button
                    className={`small-button${pageNumber === effectivePage ? ' is-active' : ''}`}
                    type="button"
                    onClick={() => updateListParams({ page: pageNumber }, false)}
                  >
                    {pageNumber}
                  </button>
                </span>
              );
            })}
            <button className="small-button" type="button" disabled={effectivePage >= totalPages} onClick={() => updateListParams({ page: effectivePage + 1 }, false)}>{copy.next}</button>
          </div>
          <label className="vpn-account-page-size">
            <span>{copy.perPage}</span>
            <select value={effectivePageSize} onChange={(event) => updateListParams({ pageSize: event.target.value, page: null }, false)}>
              <option value="25">25</option>
              <option value="50">50</option>
              <option value="100">100</option>
            </select>
          </label>
        </div>
      )}
    </div>
  );
}
