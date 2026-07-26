import { useEffect, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { getServer } from '../../entities/server/api/serverApi';
import {
  createVPNCoreInstallation,
  createVPNCoreOperation,
  getVPNCoreInstallation,
  type VPNCoreOperation,
} from '../../entities/server/api/vpnCoreApi';
import { ApiError } from '../../shared/api/client';
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

function supportsInstallation(capabilities?: Record<string, unknown>): boolean {
  const value = capabilities?.vpnCoreInstallationOperations;
  return Array.isArray(value) && value.includes('install_sing_box');
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() ? error.message : fallback;
}

export function ServerDetailsWithVPNCorePage() {
  const { serverId } = useParams<{ serverId: string }>();
  const [panelTarget, setPanelTarget] = useState<HTMLElement | null>(null);
  const [activeOperation, setActiveOperation] = useState<{ operation: VPNCoreOperation; startedAt: number } | null>(null);
  const [installationJobId, setInstallationJobId] = useState<string | null>(null);
  const [installationFailureCode, setInstallationFailureCode] = useState<string | null>(null);
  const text = getVPNCoreMessages(getCurrentLocale());
  const serverQuery = useQuery({
    queryKey: ['server', serverId],
    queryFn: () => getServer(serverId ?? ''),
    enabled: Boolean(serverId),
    refetchInterval: activeOperation || installationJobId ? 2_000 : 30_000,
  });
  const operationMutation = useMutation({
    mutationFn: (operation: VPNCoreOperation) => createVPNCoreOperation(serverId ?? '', operation),
    onSuccess: (_, operation) => {
      setActiveOperation({ operation, startedAt: Date.now() });
      void serverQuery.refetch();
    },
  });
  const installationMutation = useMutation({
    mutationFn: () => createVPNCoreInstallation(serverId ?? ''),
    onSuccess: ({ job }) => {
      setInstallationFailureCode(null);
      setInstallationJobId(job.id);
      void serverQuery.refetch();
    },
    onError: (error) => {
      setInstallationFailureCode(error instanceof ApiError ? error.code ?? 'installation_failed' : 'installation_failed');
    },
  });
  const installationQuery = useQuery({
    queryKey: ['vpn-core-installation', serverId, installationJobId],
    queryFn: () => getVPNCoreInstallation(serverId ?? '', installationJobId ?? ''),
    enabled: Boolean(serverId && installationJobId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'failed' || status === 'succeeded' ? false : 2_000;
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
  const installationSupported = supportsInstallation(agent?.capabilities);

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

  useEffect(() => {
    const job = installationQuery.data;
    if (!installationJobId || !job) return;
    if (job.status === 'failed') {
      setInstallationFailureCode(
        typeof job.errorMessage === 'string' && job.errorMessage.trim()
          ? job.errorMessage.trim()
          : 'installation_failed',
      );
      setInstallationJobId(null);
      return;
    }
    if (job.status === 'succeeded') {
      void serverQuery.refetch();
      if (status?.installed) {
        setInstallationJobId(null);
      }
    }
  }, [installationJobId, installationQuery.data?.status, status?.installed]);

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

  const installationJob = installationQuery.data;
  const installationBusy = installationMutation.isPending || installationJobId !== null;
  const busy = operationMutation.isPending || activeOperation !== null || installationBusy;
  const runOperation = (operation: VPNCoreOperation) => {
    if (operation === 'stop' && !window.confirm(text.confirmStop)) return;
    operationMutation.reset();
    operationMutation.mutate(operation);
  };

  const runInstallation = () => {
    if (!window.confirm(text.confirmInstall)) return;
    installationMutation.reset();
    setInstallationFailureCode(null);
    installationMutation.mutate();
  };

  const installationError = (() => {
    const code = installationQuery.isError ? 'installation_failed' : installationFailureCode;
    switch (code) {
      case 'agent_installation_unsupported':
        return text.installationUnsupported;
      case 'unsupported_platform':
      case 'unsupported_distribution':
      case 'unsupported_architecture':
        return text.unsupportedPlatform;
      case 'repository_configuration_failed':
      case 'signing_key_download_failed':
      case 'signing_key_download_timeout':
      case 'signing_key_conflict':
      case 'repository_source_conflict':
        return text.repositoryConfigurationFailed;
      case 'package_index_refresh_failed':
      case 'package_installation_failed':
      case 'service_start_guard_failed':
      case 'service_start_guard_cleanup_failed':
        return text.packageInstallationFailed;
      case 'installed_binary_not_found':
      case 'binary_verification_failed':
      case 'binary_version_unavailable':
      case 'service_verification_failed':
        return text.installationVerificationFailed;
      default:
        return code ? text.installationFailed : null;
    }
  })();

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

  const installationControl = status?.state === 'not_installed' && installationSupported ? (
    <div className="form-actions">
      <button className="primary-button" type="button" disabled={busy} onClick={runInstallation}>
        {installationBusy ? text.installationPending : text.installAction}
      </button>
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
          {installationJobId && installationJob?.status !== 'succeeded' && (
            <span className="muted-text">{text.installationQueued}</span>
          )}
          {installationJobId && installationJob?.status === 'succeeded' && !status?.installed && (
            <span className="muted-text">{text.installationAwaitingHeartbeat}</span>
          )}
          {operationMutation.isError && (
            <span className="form-message form-message-error">
              {errorMessage(operationMutation.error, text.operationFailed)}
            </span>
          )}
          {installationError && (
            <span className="form-message form-message-error">{installationError}</span>
          )}
          {installationControl}
          {controls}
          {!isDisconnected && status?.state === 'not_installed' && !installationSupported && (
            <span className="muted-text">{text.installationUnsupported}</span>
          )}
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
