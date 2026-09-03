import { useMutation, useQuery } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { getServers, type Server } from '../../entities/server/api/serverApi';
import {
  advancePlatformUpdateRollout,
  createPlatformUpdateRollout,
  getPlatformUpdateRollout,
  type AdvancePlatformUpdateRolloutResponse,
  type PlatformUpdateRollout,
} from '../../entities/system/api/platformUpdateRolloutApi';
import { ApiError } from '../../shared/api/client';
import { getCurrentLocale, t, type TranslationKey } from '../../shared/i18n/i18n';
import { StatusBadge } from '../../shared/ui/StatusBadge';
import {
  advanceFailureRequiresDurableRefresh,
  appendSelectedServers,
  isCanonicalRouteGateVersion,
  isCanonicalUuid,
  MAX_PLATFORM_UPDATE_ROLLOUT_MEMBERS,
  moveSelectedServer,
  parseStoredRolloutCreateAttempt,
  rolloutNextAction,
  type StoredRolloutCreateAttempt,
} from './platformUpdateRolloutModel';
import './FleetUpdateRolloutPanel.css';

const activeRolloutStorageKey = 'routegate.update.rollout.active-id.v1';
const createAttemptStorageKey = 'routegate.update.rollout.create-attempt.v1';

function readStoredAttempt(): StoredRolloutCreateAttempt | null {
  if (typeof window === 'undefined') {
    return null;
  }
  try {
    return parseStoredRolloutCreateAttempt(window.localStorage.getItem(createAttemptStorageKey));
  } catch {
    return null;
  }
}

function readActiveRolloutId(): string {
  if (typeof window === 'undefined') {
    return '';
  }
  try {
    const value = window.localStorage.getItem(activeRolloutStorageKey) ?? '';
    return isCanonicalUuid(value) ? value : '';
  } catch {
    return '';
  }
}

function formatDateTime(value?: string | null): string {
  if (!value) {
    return t('rollout.notStarted');
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(getCurrentLocale() === 'ru' ? 'ru-RU' : 'en-US', {
    dateStyle: 'medium',
    timeStyle: 'medium',
  }).format(parsed);
}

function translatedCode(prefix: 'status' | 'action' | 'reason', value: string): string {
  const key = `rollout.${prefix}.${value}` as TranslationKey;
  const translated = t(key);
  return translated === key ? value : translated;
}

function errorMessage(error: unknown, fallback: TranslationKey): string {
  return error instanceof ApiError && error.message.trim() !== '' ? error.message : t(fallback);
}

function advanceOutcomeIsAmbiguous(error: unknown): boolean {
  return advanceFailureRequiresDurableRefresh(error instanceof ApiError ? error.status : undefined);
}

function NodeIdentity({ server, serverId }: { server?: Server; serverId: string }) {
  return (
    <div className="rollout-node-identity">
      <strong>{server?.name ?? serverId}</strong>
      <code>{serverId}</code>
    </div>
  );
}

function terminalMessage(rollout: PlatformUpdateRollout): string | null {
  switch (rollout.status) {
    case 'succeeded':
      return t('rollout.terminalSucceeded');
    case 'failed':
      return t('rollout.terminalFailed');
    case 'outcome_unknown':
      return t('rollout.terminalUnknown');
    default:
      return null;
  }
}

export function FleetUpdateRolloutPanel({ managerVersion }: { managerVersion: string }) {
  const [storedAttempt, setStoredAttempt] = useState<StoredRolloutCreateAttempt | null>(readStoredAttempt);
  const [selectedServerIds, setSelectedServerIds] = useState<string[]>(() => storedAttempt?.serverIds ?? []);
  const [activeRolloutId, setActiveRolloutId] = useState(readActiveRolloutId);
  const [resumeId, setResumeId] = useState('');
  const [resumeInvalid, setResumeInvalid] = useState(false);
  const [discardConfirming, setDiscardConfirming] = useState(false);
  const [storageError, setStorageError] = useState(false);
  const [advanceRecoveryRequired, setAdvanceRecoveryRequired] = useState(false);
  const [lastStep, setLastStep] = useState<AdvancePlatformUpdateRolloutResponse | null>(null);

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
    refetchInterval: 30_000,
  });
  const rolloutQuery = useQuery({
    queryKey: ['platform-update-rollout', activeRolloutId],
    queryFn: () => getPlatformUpdateRollout(activeRolloutId),
    enabled: activeRolloutId !== '',
    retry: false,
    refetchOnWindowFocus: false,
  });

  const servers = serversQuery.data?.items ?? [];
  const vpnServers = useMemo(
    () => servers.filter((server) => server.deploymentRole === 'vpn'),
    [servers],
  );
  const serverById = useMemo(
    () => new Map(servers.map((server) => [server.id, server])),
    [servers],
  );
  const selectableServers = vpnServers.filter((server) => !selectedServerIds.includes(server.id));
  const targetVersion = storedAttempt?.targetVersion ?? managerVersion;
  const targetValid = isCanonicalRouteGateVersion(targetVersion);

  function persistActiveRollout(id: string): void {
    setActiveRolloutId(id);
    try {
      if (id) {
        window.localStorage.setItem(activeRolloutStorageKey, id);
      } else {
        window.localStorage.removeItem(activeRolloutStorageKey);
      }
    } catch {
      setStorageError(true);
    }
  }

  function clearStoredAttempt(): void {
    try {
      window.localStorage.removeItem(createAttemptStorageKey);
    } catch {
      setStorageError(true);
    }
    setStoredAttempt(null);
    setDiscardConfirming(false);
  }

  const createMutation = useMutation({
    mutationFn: (attempt: StoredRolloutCreateAttempt) => createPlatformUpdateRollout(
      { targetVersion: attempt.targetVersion, serverIds: attempt.serverIds },
      attempt.idempotencyKey,
    ),
    retry: false,
    onSuccess: ({ rollout }) => {
      clearStoredAttempt();
      persistActiveRollout(rollout.id);
      setLastStep(null);
      setAdvanceRecoveryRequired(false);
    },
  });

  const advanceMutation = useMutation({
    mutationFn: () => advancePlatformUpdateRollout(activeRolloutId),
    retry: false,
    onSuccess: async (result) => {
      setLastStep(result);
      const refreshed = await rolloutQuery.refetch();
      setAdvanceRecoveryRequired(!refreshed.isSuccess);
    },
    onError: (error) => {
      if (advanceOutcomeIsAmbiguous(error)) {
        setAdvanceRecoveryRequired(true);
      }
    },
  });

  function addServer(serverId: string): void {
    if (storedAttempt || selectedServerIds.length >= MAX_PLATFORM_UPDATE_ROLLOUT_MEMBERS) {
      return;
    }
    setSelectedServerIds((current) => current.includes(serverId) ? current : [...current, serverId]);
  }

  function startCreate(): void {
    setStorageError(false);
    createMutation.reset();
    if (storedAttempt) {
      createMutation.mutate(storedAttempt);
      return;
    }
    if (!targetValid || selectedServerIds.length === 0) {
      return;
    }
    const attempt: StoredRolloutCreateAttempt = {
      idempotencyKey: crypto.randomUUID(),
      targetVersion,
      serverIds: [...selectedServerIds],
    };
    try {
      window.localStorage.setItem(createAttemptStorageKey, JSON.stringify(attempt));
    } catch {
      setStorageError(true);
      return;
    }
    setStoredAttempt(attempt);
    createMutation.mutate(attempt);
  }

  function resumeRollout(): void {
    const id = resumeId.trim();
    if (!isCanonicalUuid(id)) {
      setResumeInvalid(true);
      return;
    }
    setResumeInvalid(false);
    setLastStep(null);
    setAdvanceRecoveryRequired(false);
    persistActiveRollout(id);
  }

  async function refreshDurableState(): Promise<void> {
    advanceMutation.reset();
    const result = await rolloutQuery.refetch();
    if (result.isSuccess) {
      setAdvanceRecoveryRequired(false);
    }
  }

  const rollout = rolloutQuery.data?.rollout;
  const nextAction = rollout ? rolloutNextAction(rollout) : null;
  const terminalCopy = rollout ? terminalMessage(rollout) : null;
  const terminalEntries = rollout?.entries.filter((entry) => (
    entry.status === 'healthy'
    || entry.status === 'failed'
    || entry.status === 'outcome_unknown'
    || entry.status === 'skipped'
  )).length ?? 0;

  return (
    <section className="panel settings-panel settings-rollout-panel">
      <div className="settings-panel-heading">
        <div>
          <div className="panel-title">{t('rollout.title')}</div>
          <p className="panel-subtitle">{t('rollout.subtitle')}</p>
        </div>
        {rollout && (
          <StatusBadge
            status={rollout.status}
            label={translatedCode('status', rollout.status)}
          />
        )}
      </div>

      {storageError && <div className="form-message form-message-error">{t('rollout.storageError')}</div>}

      {storedAttempt && (
        <div className="rollout-recovery rollout-recovery-warning">
          <strong>{t('rollout.createRecoveryTitle')}</strong>
          <p>{t('rollout.createRecoveryHint')}</p>
          <div className="rollout-recovery-request">
            <code>{storedAttempt.targetVersion}</code>
            <span>{t('rollout.selectedCount', { count: storedAttempt.serverIds.length })}</span>
          </div>
          <ol className="rollout-recovery-members">
            {storedAttempt.serverIds.map((serverId) => (
              <li key={serverId}><NodeIdentity server={serverById.get(serverId)} serverId={serverId} /></li>
            ))}
          </ol>
          {!discardConfirming ? (
            <div className="rollout-actions">
              <button className="button button-primary" disabled={createMutation.isPending} onClick={startCreate} type="button">
                {createMutation.isPending ? t('rollout.creating') : t('rollout.retryCreate')}
              </button>
              <button className="button button-secondary" disabled={createMutation.isPending} onClick={() => setDiscardConfirming(true)} type="button">
                {t('rollout.discardRecovery')}
              </button>
            </div>
          ) : (
            <div className="rollout-discard-confirm">
              <strong>{t('rollout.discardConfirmTitle')}</strong>
              <p>{t('rollout.discardConfirmHint')}</p>
              <div className="rollout-actions">
                <button className="button button-secondary" onClick={() => setDiscardConfirming(false)} type="button">{t('rollout.keepRecovery')}</button>
                <button className="button button-primary" onClick={() => { clearStoredAttempt(); createMutation.reset(); }} type="button">{t('rollout.confirmDiscard')}</button>
              </div>
            </div>
          )}
          {createMutation.isError && (
            <div className="form-message form-message-error">
              {errorMessage(createMutation.error, 'rollout.createError')}
            </div>
          )}
        </div>
      )}

      {!activeRolloutId && !storedAttempt && (
        <div className="rollout-builder">
          <div className="rollout-target">
            <span>{t('rollout.targetVersion')}</span>
            <code>{managerVersion || '—'}</code>
            <small>{t('rollout.managerTargetHint')}</small>
          </div>
          {!targetValid && <div className="form-message form-message-warning">{t('rollout.invalidManagerVersion')}</div>}
          {serversQuery.isError && <div className="form-message form-message-error">{t('rollout.nodesLoadError')}</div>}

          <div className="rollout-selection-grid">
            <section className="rollout-selection-column">
              <div className="rollout-selection-heading">
                <strong>{t('rollout.availableNodes')}</strong>
                <button
                  className="small-button"
                  disabled={selectableServers.length === 0 || selectedServerIds.length >= MAX_PLATFORM_UPDATE_ROLLOUT_MEMBERS}
                  onClick={() => setSelectedServerIds((current) => appendSelectedServers(current, vpnServers.map((server) => server.id)))}
                  type="button"
                >
                  {t('rollout.selectAll')}
                </button>
              </div>
              {serversQuery.isSuccess && vpnServers.length === 0 && <p className="empty-state">{t('rollout.noVpnNodes')}</p>}
              <div className="rollout-node-list">
                {selectableServers.map((server) => (
                  <div className="rollout-candidate" key={server.id}>
                    <NodeIdentity server={server} serverId={server.id} />
                    <span>{t('rollout.nodeStatus', { server: translatedCode('status', server.status), agent: translatedCode('status', server.agent?.status ?? 'unknown') })}</span>
                    <button className="small-button" onClick={() => addServer(server.id)} type="button">{t('rollout.addNode')}</button>
                  </div>
                ))}
              </div>
            </section>

            <section className="rollout-selection-column">
              <div className="rollout-selection-heading">
                <strong>{t('rollout.selectedNodes')}</strong>
                <button className="small-button" disabled={selectedServerIds.length === 0} onClick={() => setSelectedServerIds([])} type="button">{t('rollout.clearSelection')}</button>
              </div>
              <span className="rollout-selection-count">{t('rollout.selectedCount', { count: selectedServerIds.length })}</span>
              <ol className="rollout-ordered-list">
                {selectedServerIds.map((serverId, index) => {
                  const server = serverById.get(serverId);
                  const nodeName = server?.name ?? serverId;
                  return (
                    <li key={serverId}>
                      <NodeIdentity server={server} serverId={serverId} />
                      <div className="rollout-order-actions">
                        <button aria-label={t('rollout.moveUp', { node: nodeName })} className="small-button" disabled={index === 0} title={t('rollout.moveUp', { node: nodeName })} onClick={() => setSelectedServerIds((current) => moveSelectedServer(current, serverId, -1))} type="button">↑</button>
                        <button aria-label={t('rollout.moveDown', { node: nodeName })} className="small-button" disabled={index === selectedServerIds.length - 1} title={t('rollout.moveDown', { node: nodeName })} onClick={() => setSelectedServerIds((current) => moveSelectedServer(current, serverId, 1))} type="button">↓</button>
                        <button aria-label={t('rollout.removeNode', { node: nodeName })} className="small-button" title={t('rollout.removeNode', { node: nodeName })} onClick={() => setSelectedServerIds((current) => current.filter((id) => id !== serverId))} type="button">×</button>
                      </div>
                    </li>
                  );
                })}
              </ol>
              <p className="rollout-order-hint">{t('rollout.orderHint')}</p>
            </section>
          </div>

          <div className="rollout-create-row">
            <button className="button button-primary" disabled={!targetValid || selectedServerIds.length === 0 || createMutation.isPending} onClick={startCreate} type="button">
              {createMutation.isPending ? t('rollout.creating') : t('rollout.create')}
            </button>
            <span>{t('rollout.createHint')}</span>
          </div>
        </div>
      )}

      {!activeRolloutId && !storedAttempt && (
        <div className="rollout-resume">
          <div>
            <strong>{t('rollout.resumeTitle')}</strong>
            <p>{t('rollout.resumeHint')}</p>
          </div>
          <label className="field">
            <span>{t('rollout.rolloutId')}</span>
            <input
              aria-invalid={resumeInvalid}
              onChange={(event) => { setResumeId(event.target.value); setResumeInvalid(false); }}
              placeholder={t('rollout.rolloutIdPlaceholder')}
              value={resumeId}
            />
          </label>
          {resumeInvalid && <div className="form-message form-message-error">{t('rollout.invalidRolloutId')}</div>}
          <button className="button button-secondary" onClick={resumeRollout} type="button">{t('rollout.resume')}</button>
        </div>
      )}

      {activeRolloutId && (
        <div className="rollout-view">
          <div className="rollout-view-toolbar">
            <code>{activeRolloutId}</code>
            <div className="rollout-actions">
              <button className="button button-secondary" disabled={rolloutQuery.isFetching || advanceMutation.isPending} onClick={refreshDurableState} type="button">{t('rollout.refresh')}</button>
              <button className="button button-secondary" disabled={advanceMutation.isPending} onClick={() => { persistActiveRollout(''); setLastStep(null); setAdvanceRecoveryRequired(false); }} type="button">{t('rollout.stopViewing')}</button>
            </div>
          </div>

          {rolloutQuery.isLoading && <div className="settings-update-progress" role="status">{t('rollout.loading')}</div>}
          {rolloutQuery.isError && <div className="form-message form-message-error">{t('rollout.loadError')}</div>}

          {rollout && (
            <>
              <div className="rollout-facts">
                <div><span>{t('rollout.targetVersion')}</span><strong>{rollout.targetVersion}</strong></div>
                <div><span>{t('rollout.createdAt')}</span><strong>{formatDateTime(rollout.createdAt)}</strong></div>
                <div><span>{t('rollout.startedAt')}</span><strong>{formatDateTime(rollout.startedAt)}</strong></div>
                <div><span>{t('rollout.completedAt')}</span><strong>{formatDateTime(rollout.completedAt)}</strong></div>
              </div>

              <div className="rollout-progress-block">
                <div><strong>{t('rollout.progress')}</strong><span>{t('rollout.progressValue', { completed: terminalEntries, total: rollout.entries.length })}</span></div>
                <progress max={Math.max(rollout.entries.length, 1)} value={terminalEntries} />
              </div>

              {rollout.errorCode && <div className="form-message form-message-error"><strong>{t('rollout.errorCode')}:</strong> {translatedCode('reason', rollout.errorCode)}</div>}
              {terminalCopy && <div className={`rollout-terminal rollout-terminal-${rollout.status}`}>{terminalCopy}</div>}

              <div className="rollout-entry-section">
                <strong>{t('rollout.entriesTitle')}</strong>
                <ol className="rollout-entry-list">
                  {[...rollout.entries].sort((left, right) => left.position - right.position).map((entry) => (
                    <li key={entry.serverId}>
                      <div className="rollout-entry-heading">
                        <NodeIdentity server={serverById.get(entry.serverId)} serverId={entry.serverId} />
                        <StatusBadge status={entry.status} label={translatedCode('status', entry.status)} />
                      </div>
                      {entry.jobId && <div className="rollout-entry-detail"><span>{t('rollout.jobId')}</span><code>{entry.jobId}</code></div>}
                      {entry.completedAt && <div className="rollout-entry-detail"><span>{t('rollout.completedAt')}</span><strong>{formatDateTime(entry.completedAt)}</strong></div>}
                      {entry.planningBlockers.length > 0 && (
                        <div className="rollout-entry-reasons">
                          <span>{t('rollout.planningBlockers')}</span>
                          <div>{entry.planningBlockers.map((reason) => <em key={reason}>{translatedCode('reason', reason)}</em>)}</div>
                        </div>
                      )}
                      {entry.blockerCode && <div className="rollout-entry-detail"><span>{t('rollout.blockerCode')}</span><strong>{translatedCode('reason', entry.blockerCode)}</strong></div>}
                    </li>
                  ))}
                </ol>
              </div>

              {lastStep && (
                <div className="rollout-last-step">
                  <strong>{t('rollout.lastAction')}</strong>
                  <span>{translatedCode('action', lastStep.action)}</span>
                  {lastStep.waitingReason && <span>{t('rollout.waitingReason')}: {translatedCode('reason', lastStep.waitingReason)}</span>}
                  {lastStep.errorCode && <span>{t('rollout.errorCode')}: {translatedCode('reason', lastStep.errorCode)}</span>}
                  {lastStep.blockerCode && <span>{t('rollout.blockerCode')}: {translatedCode('reason', lastStep.blockerCode)}</span>}
                </div>
              )}

              {advanceRecoveryRequired ? (
                <div className="rollout-recovery rollout-recovery-warning">
                  <strong>{t('rollout.advanceRecoveryTitle')}</strong>
                  <p>{t('rollout.advanceRecoveryHint')}</p>
                  <button className="button button-primary" disabled={rolloutQuery.isFetching} onClick={refreshDurableState} type="button">{t('rollout.reloadAfterAmbiguous')}</button>
                </div>
              ) : nextAction && (
                <div className="rollout-advance">
                  <p>{t('rollout.advanceHint')}</p>
                  <button className="button button-primary" disabled={advanceMutation.isPending || rolloutQuery.isFetching} onClick={() => { advanceMutation.reset(); advanceMutation.mutate(); }} type="button">
                    {advanceMutation.isPending
                      ? t('rollout.advancing')
                      : t(nextAction === 'start' ? 'rollout.advanceStart' : nextAction === 'check' ? 'rollout.advanceCheck' : 'rollout.advanceContinue')}
                  </button>
                </div>
              )}

              {advanceMutation.isError && !advanceRecoveryRequired && (
                <div className="form-message form-message-error">{errorMessage(advanceMutation.error, 'rollout.advanceError')}</div>
              )}
            </>
          )}
        </div>
      )}
    </section>
  );
}
