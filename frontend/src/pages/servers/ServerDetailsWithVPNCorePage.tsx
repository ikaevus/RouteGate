import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { useQuery } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { getServer } from '../../entities/server/api/serverApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';
import { parseVPNCoreStatus } from '../../entities/server/model/vpnCoreStatus';
import { ServerDetailsPage as LegacyServerDetailsPage } from './ServerDetailsLegacyPage';
import { getVPNCoreMessages } from './vpnCoreMessages';

function valueOrFallback(value: string | null | undefined, fallback: string): string {
  return value?.trim() ? value : fallback;
}

function formatDate(value: string | null | undefined, fallback: string): string {
  if (!value) {
    return fallback;
  }

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

export function ServerDetailsWithVPNCorePage() {
  const { serverId } = useParams<{ serverId: string }>();
  const [panelTarget, setPanelTarget] = useState<HTMLElement | null>(null);
  const text = getVPNCoreMessages(getCurrentLocale());
  const serverQuery = useQuery({
    queryKey: ['server', serverId],
    queryFn: () => getServer(serverId ?? ''),
    enabled: Boolean(serverId),
    refetchInterval: 30_000,
  });

  useEffect(() => {
    setPanelTarget(document.querySelector<HTMLElement>('.details-layout'));
  }, [serverId, serverQuery.isSuccess]);

  const agent = serverQuery.data?.agent;
  const status = parseVPNCoreStatus(agent?.capabilities);
  const isDisconnected = !agent;

  let title = text.unknownTitle;
  let description = text.unknownDescription;
  let nextAction = text.retryAction;
  let badgeLabel = title;
  let tone = 'unknown';
  let canRetry = true;

  if (isDisconnected) {
    title = text.connectedFirst;
    description = text.connectedFirstDescription;
    nextAction = '';
    badgeLabel = text.unavailableStatus;
    tone = 'pending';
    canRetry = false;
  } else if (!status) {
    title = text.legacyTitle;
    description = text.legacyDescription;
    nextAction = text.updateAction;
    badgeLabel = title;
    tone = 'upgrade-recommended';
    canRetry = false;
  } else {
    tone = status.state;
    switch (status.state) {
      case 'not_installed':
        title = text.notInstalledTitle;
        description = text.notInstalledDescription;
        nextAction = text.installAction;
        break;
      case 'running':
        title = text.runningTitle;
        description = text.runningDescription;
        nextAction = text.plannedAction;
        break;
      case 'stopped':
        title = text.stoppedTitle;
        description = text.stoppedDescription;
        nextAction = text.startAction;
        break;
      case 'failed':
      case 'degraded':
        title = text.failedTitle;
        description = text.failedDescription;
        nextAction = text.retryAction;
        break;
      case 'installed':
        title = text.installedTitle;
        description = text.installedDescription;
        nextAction = text.startAction;
        break;
      default:
        break;
    }

    badgeLabel = title;
    canRetry = status.state === 'failed' || status.state === 'degraded' || status.state === 'unknown';
  }

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
          {canRetry ? (
            <button
              className="primary-button"
              type="button"
              disabled={serverQuery.isFetching}
              onClick={() => void serverQuery.refetch()}
            >
              {serverQuery.isFetching ? text.checkingAction : nextAction}
            </button>
          ) : (
            nextAction && <span className="muted-text">{nextAction}</span>
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
