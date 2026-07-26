import { useEffect, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { getServer } from '../../entities/server/api/serverApi';
import {
  createVPNCoreOperation,
  type VPNCoreOperation,
} from '../../entities/server/api/vpnCoreApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';
import { parseVPNCoreStatus } from '../../entities/server/model/vpnCoreStatus';
import { ServerDetailsPage as LegacyServerDetailsPage } from './ServerDetailsLegacyPage';
import { getVPNCoreMessages } from './vpnCoreMessages';

function valueOrFallback(value: string | null | undefined, fallback: string): string {
  return value?.trim() ? value : fallback;
}

function formatDate(value: string | null | undefined, fallback: string): string {
  if (!value) return fallback;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function supportedOperations(capabilities?: Record<string, unknown>): Set<VPNCoreOperation> {
  const value = capabilities?.vpnCoreServiceOperations;
  if (!Array.isArray(value)) return new Set();
  return new Set(value.filter((item): item is VPNCoreOperation =>
    item === 'start' || item === 'stop' || item === 'restart'));
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() ? error.message : fallback;
}

export function ServerDetailsWithVPNCorePage() {
  const { serverId } = useParams<{ serverId: string }>();
  const [panelTarget, setPanelTarget] = useState<HTMLElement | null>(null);
  const [activeOperation, setActiveOperation] = useState<{ operation: VPNCoreOperation; startedAt: number } | null>(null);
  const text = getVPNCoreMessages(getCurrentLocale());
  const serverQuery = useQuery({
    queryKey: ['server', serverId],
    queryFn: () => getServer(serverId ?? ''),
    enabled: Boolean(serverId),
    refetchInterval: activeOperation ? 2_000 : 30_000,
  });
  const operationMutation = useMutation({
    mutationFn: (operation: VPNCoreOperation) => createVPNCoreOperation(serverId ?? '', operation),
    onSuccess: (_, operation) => {
      setActiveOperation({ operation, startedAt: Date.now() });
      void serverQuery.refetch();
    },
  });

  useEffect(() => {
    setPanelTarget(document.querySelector<HTMLElement>('.details-layout'));
  }, [serverId, serverQuery.isSuccess]);

  const agent = serverQuery.data?.agent;
  const status = parseVPNCoreStatus(agent?.capabilities);
  const isDisconnected = !agent;
  const operations = useMemo(() => supportedOperations(agent?.capabilities), [agent?.capabilities]);
  const controlsSupported = operations.size > 0;

  useEffect(() => {
    if (!activeOperation || !status) return;
    if (Date.now() - activeOperation.startedAt < 2_500) return;
    const reachedExpectedState =
      (activeOperation.operation === 'stop' && status.state === 'stopped') ||
      ((activeOperation.operation === 'start' || activeOperation.operation === 'restart') && status.state === 'running');
    if (reachedExpectedState) {
      setActiveOperation(null);
    }
  }, [activeOperation, status]);

  let title = text.unknownTitle;
  let description = text.unknownDescription;
  let badgeLabel = title;
  let tone = 'unknown';
  let canRetry = true;

  if (isDisconnected) {
    title = text.connectedFirst;
    description = text.connectedFirstDescription;
    badgeLabel = text.unavailableStatus;
    tone = 'pending';
    canRetry = false;
  } else if (!status) {
    title = text.legacyTitle;
    description = text.legacyDescription;
    badgeLabel = text.legacyStatus;
    tone = 'pending';
    canRetry = false;
  } else {
    tone = status.state;
    switch (status.state) {
      case 'not_installed':
        title = text.notInstalledTitle;
        description = text.notInstalledDescription;
        break;
      case 'running':
        title = text.runningTitle;
        description = text.runningDescription;
        break;
      case 'stopped':
        title = text.stoppedTitle;
        description = text.stoppedDescription;
        break;
      case 'failed':
      case 'degraded':
        title = text.failedTitle;
        description = text.failedDescription;
        break;
      case 'installed':
        title = text.installedTitle;
        description = text.installedDescription;
        break;
      default:
        break;
    }
    badgeLabel = title;
    canRetry = status.state === 'failed' || status.state === 'degraded' || status.state === 'unknown';
  }

  const busy = operationMutation.isPending || activeOperation !== null;
  const runOperation = (operation: VPNCoreOperation) => {
    if (operation === 'stop' && !window.confirm(text.confirmStop)) return;
    operationMutation.reset();
    operationMutation.mutate(operation);
  };

  const controls = status && status.installed && controlsSupported ? (
    <div className="form-actions">
      {(status.state === 'stopped' || status.state === 'installed' || status.state === 'failed') && operations.has('start') && (
        <button className="primary-button" type="button" disabled={busy} onClick={() => runOperation('start')}>
          {busy ? text.operationPending : text.startAction}
        </button>
      )}
      {status.state === 'running' && operations.has('stop') && (
        <button className="secondary-button" type="button" disabled={busy} onClick={() => runOperation('stop')}>
          {text.stopAction}
        </button>
      )}
      {(status.state === 'running' || status.state === 'failed' || status.state === 'degraded') && operations.has('restart') && (
        <button className="secondary-button" type="button" disabled={busy} onClick={() => runOperation('restart')}>
          {text.restartAction}
        </button>
      )}
    </div>
  ) : null;

  const panel = (
    <section className="vpn-core-management-section" style={{ gridColumn: '1 / -1' }}>
      <div className="panel vpn-core-status-panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">{text.title}</div>
            <p className="panel-subtitle">{text.subtitle}</p>
          </div>
          <span className={`badge badge-${tone.replace(/[^a-z0-9-]/g, '-')}`}>{badgeLabel}</span>
        </div>

        <div className="empty-state empty-state-card">
          {!isDisconnected && <strong>{title}</strong>}
          <span>{description}</span>
          {activeOperation && <span className="muted-text">{text.operationQueued}</span>}
          {operationMutation.isError && (
            <span className="form-message form-message-error">
              {errorMessage(operationMutation.error, text.operationFailed)}
            </span>
          )}
          {controls}
          {!isDisconnected && status?.installed && !controlsSupported && (
            <span className="muted-text">{text.unsupportedControls}</span>
          )}
          {canRetry && !busy && (
            <button className="secondary-button" type="button" disabled={serverQuery.isFetching} onClick={() => void serverQuery.refetch()}>
              {serverQuery.isFetching ? text.checkingAction : text.retryAction}
            </button>
          )}
        </div>

        {status && (
          <details className="server-connection-technical-details">
            <summary>{text.technicalDetails}</summary>
            <div className="detail-list">
              <div className="detail-row"><span>{text.version}</span><strong>{valueOrFallback(status.version, text.notAvailable)}</strong></div>
              <div className="detail-row"><span>{text.service}</span><strong>{valueOrFallback(status.serviceName, text.notAvailable)}</strong></div>
              <div className="detail-row"><span>{text.serviceState}</span><strong>{valueOrFallback(status.serviceState, text.notAvailable)}</strong></div>
              <div className="detail-row"><span>{text.binaryPath}</span><strong>{valueOrFallback(status.binaryPath, text.notAvailable)}</strong></div>
              <div className="detail-row"><span>{text.checkedAt}</span><strong>{formatDate(status.checkedAt, text.notAvailable)}</strong></div>
            </div>
          </details>
        )}
      </div>
    </section>
  );

  return (
    <>
      <LegacyServerDetailsPage />
      {panelTarget ? createPortal(panel, panelTarget) : null}
    </>
  );
}
