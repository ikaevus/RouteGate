import { useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { getAgents } from '../entities/agent/api/agentApi';
import { getServers } from '../entities/server/api/serverApi';
import { getPagedVpnAccounts } from '../entities/vpnAccount/api/vpnAccountManagementApi';
import { t } from '../shared/i18n/i18n';
import { useLocale } from '../shared/i18n/useLocale';
import './GlobalSearchController.css';

type SearchResult = {
  key: string;
  kind: 'server' | 'agent' | 'vpn-account';
  label: string;
  meta: string;
  path: string;
};

type DropdownPosition = {
  left: number;
  top: number;
  width: number;
};

const MIN_QUERY_LENGTH = 2;
const MAX_RESULTS_PER_GROUP = 4;

function normalize(value: unknown): string {
  return String(value ?? '').trim().toLocaleLowerCase();
}

function matches(query: string, ...values: unknown[]): boolean {
  return values.some((value) => normalize(value).includes(query));
}

function compactMeta(...values: Array<string | null | undefined>): string {
  return values.map((value) => value?.trim()).filter(Boolean).join(' · ');
}

export function GlobalSearchController() {
  const navigate = useNavigate();
  useLocale();
  const [input, setInput] = useState<HTMLInputElement | null>(null);
  const [query, setQuery] = useState('');
  const [debouncedQuery, setDebouncedQuery] = useState('');
  const [isFocused, setIsFocused] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const [position, setPosition] = useState<DropdownPosition | null>(null);
  const queryRef = useRef(query);
  queryRef.current = query;

  useEffect(() => {
    const findInput = () => {
      const nextInput = document.querySelector<HTMLInputElement>('.admin-topbar .topbar-search input');
      setInput((current) => (current === nextInput ? current : nextInput));
    };

    findInput();
    const observer = new MutationObserver(findInput);
    observer.observe(document.body, { childList: true, subtree: true });
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (!input) return;

    input.setAttribute('autocomplete', 'off');
    input.setAttribute('role', 'combobox');
    input.setAttribute('aria-autocomplete', 'list');
    input.setAttribute('aria-controls', 'routegate-global-search-results');

    const handleInput = () => setQuery(input.value);
    const handleFocus = () => setIsFocused(true);
    const handleBlur = () => {
      window.setTimeout(() => setIsFocused(false), 120);
    };

    input.addEventListener('input', handleInput);
    input.addEventListener('focus', handleFocus);
    input.addEventListener('blur', handleBlur);

    return () => {
      input.removeEventListener('input', handleInput);
      input.removeEventListener('focus', handleFocus);
      input.removeEventListener('blur', handleBlur);
    };
  }, [input]);

  useEffect(() => {
    const handleShortcut = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey) || event.key.toLocaleLowerCase() !== 'k') return;
      const target = document.querySelector<HTMLInputElement>('.admin-topbar .topbar-search input');
      if (!target) return;
      event.preventDefault();
      target.focus();
      target.select();
    };

    document.addEventListener('keydown', handleShortcut);
    return () => document.removeEventListener('keydown', handleShortcut);
  }, []);

  useEffect(() => {
    const timeoutId = window.setTimeout(() => setDebouncedQuery(query.trim()), 180);
    return () => window.clearTimeout(timeoutId);
  }, [query]);

  const normalizedQuery = normalize(debouncedQuery);
  const isSearchActive = normalizedQuery.length >= MIN_QUERY_LENGTH;

  const serversQuery = useQuery({
    queryKey: ['global-search', 'servers'],
    queryFn: getServers,
    enabled: isSearchActive,
    staleTime: 30_000,
  });

  const agentsQuery = useQuery({
    queryKey: ['global-search', 'agents'],
    queryFn: getAgents,
    enabled: isSearchActive,
    staleTime: 30_000,
  });

  const accountsQuery = useQuery({
    queryKey: ['global-search', 'vpn-accounts', normalizedQuery],
    queryFn: () => getPagedVpnAccounts({ search: debouncedQuery, page: 1, pageSize: 8 }),
    enabled: isSearchActive,
    staleTime: 15_000,
  });

  const results = useMemo<SearchResult[]>(() => {
    if (!isSearchActive) return [];

    const servers = (serversQuery.data?.items ?? [])
      .filter((server) => matches(
        normalizedQuery,
        server.id,
        server.name,
        server.description,
        server.location,
        server.provider,
        server.publicIp,
        server.privateIp,
      ))
      .slice(0, MAX_RESULTS_PER_GROUP)
      .map<SearchResult>((server) => ({
        key: `server:${server.id}`,
        kind: 'server',
        label: server.name,
        meta: compactMeta(server.location, server.publicIp, server.provider) || server.id,
        path: `/servers/${encodeURIComponent(server.id)}`,
      }));

    const agents = (agentsQuery.data?.items ?? [])
      .filter((agent) => matches(
        normalizedQuery,
        agent.id,
        agent.serverId,
        agent.name,
        agent.hostname,
        agent.agentVersion,
        agent.version,
      ))
      .slice(0, MAX_RESULTS_PER_GROUP)
      .map<SearchResult>((agent) => ({
        key: `agent:${agent.id}`,
        kind: 'agent',
        label: agent.name || agent.hostname || agent.id,
        meta: compactMeta(agent.hostname, agent.agentVersion) || agent.serverId,
        path: `/servers/${encodeURIComponent(agent.serverId)}`,
      }));

    const accounts = (accountsQuery.data?.items ?? [])
      .slice(0, MAX_RESULTS_PER_GROUP)
      .map<SearchResult>((account) => ({
        key: `vpn-account:${account.id}`,
        kind: 'vpn-account',
        label: account.displayName || account.email || account.id,
        meta: compactMeta(account.email, account.status) || account.id,
        path: `/vpn-accounts/${encodeURIComponent(account.id)}`,
      }));

    return [...servers, ...agents, ...accounts];
  }, [accountsQuery.data?.items, agentsQuery.data?.items, isSearchActive, normalizedQuery, serversQuery.data?.items]);

  useEffect(() => {
    setActiveIndex((current) => Math.min(current, Math.max(0, results.length - 1)));
  }, [results.length]);

  useEffect(() => {
    if (!input || !isFocused || !isSearchActive) {
      setPosition(null);
      return;
    }

    const updatePosition = () => {
      const anchor = input.closest<HTMLElement>('.topbar-search') ?? input;
      const rect = anchor.getBoundingClientRect();
      const viewportPadding = 12;
      const preferredWidth = Math.max(rect.width, 520);
      const width = Math.min(preferredWidth, window.innerWidth - viewportPadding * 2);
      const left = Math.min(
        Math.max(viewportPadding, rect.left),
        Math.max(viewportPadding, window.innerWidth - width - viewportPadding),
      );
      setPosition({ left, top: rect.bottom + 8, width });
    };

    updatePosition();
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, true);
    return () => {
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, true);
    };
  }, [input, isFocused, isSearchActive]);

  const selectResult = (result: SearchResult) => {
    navigate(result.path);
    if (input) input.value = '';
    setQuery('');
    setDebouncedQuery('');
    setIsFocused(false);
  };

  useEffect(() => {
    if (!input) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (!isSearchActive) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        input.blur();
        return;
      }
      if (results.length === 0) return;
      if (event.key === 'ArrowDown') {
        event.preventDefault();
        setActiveIndex((current) => (current + 1) % results.length);
      } else if (event.key === 'ArrowUp') {
        event.preventDefault();
        setActiveIndex((current) => (current - 1 + results.length) % results.length);
      } else if (event.key === 'Enter') {
        event.preventDefault();
        selectResult(results[activeIndex] ?? results[0]);
      }
    };

    input.addEventListener('keydown', handleKeyDown);
    return () => input.removeEventListener('keydown', handleKeyDown);
  }, [activeIndex, input, isSearchActive, results]);

  if (!position || !isFocused || !isSearchActive) return null;

  const isLoading = serversQuery.isFetching || agentsQuery.isFetching || accountsQuery.isFetching;
  const kindLabel = (kind: SearchResult['kind']) => {
    if (kind === 'server') return t('navigation.servers');
    if (kind === 'agent') return t('navigation.agents');
    return t('navigation.vpnAccounts');
  };

  return createPortal(
    <div
      className="global-search-results"
      id="routegate-global-search-results"
      role="listbox"
      style={{ left: position.left, top: position.top, width: position.width }}
    >
      {results.length > 0 ? results.map((result, index) => (
        <button
          aria-selected={index === activeIndex}
          className={`global-search-result${index === activeIndex ? ' global-search-result-active' : ''}`}
          key={result.key}
          onMouseDown={(event) => event.preventDefault()}
          onMouseEnter={() => setActiveIndex(index)}
          onClick={() => selectResult(result)}
          role="option"
          type="button"
        >
          <span className={`global-search-kind global-search-kind-${result.kind}`}>
            {kindLabel(result.kind)}
          </span>
          <span className="global-search-result-copy">
            <strong>{result.label}</strong>
            <small>{result.meta}</small>
          </span>
          <span className="global-search-result-arrow" aria-hidden="true">→</span>
        </button>
      )) : (
        <div className="global-search-empty" aria-live="polite">
          <span>—</span>
        </div>
      )}
      {isLoading && <div className="global-search-loading" aria-hidden="true" />}
    </div>,
    document.body,
  );
}
