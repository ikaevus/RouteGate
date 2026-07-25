import { useQuery } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { getServer } from '../../entities/server/api/serverApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';
import { parseVPNCoreStatus } from '../../entities/server/model/vpnCoreStatus';
import { ServerDetailsPage } from './ServerDetailsPage';

const copy = {
  en: {
    title: 'VPN service',
    subtitle: 'RouteGate checks the VPN Core installed on this server and shows the next recommended action.',
    connectedFirst: 'Connect the server first',
    connectedFirstDescription: 'RouteGate Agent must be connected before RouteGate can inspect or manage the VPN service.',
    legacyTitle: 'Update RouteGate Agent',
    legacyDescription: 'This Agent does not report VPN Core status yet. Update it to enable guided VPN service management.',
    notInstalledTitle: 'VPN Core is not installed',
    notInstalledDescription: 'Install sing-box to prepare this server for VPN configuration deployment.',
    installedTitle: 'VPN Core is installed',
    installedDescription: 'sing-box is available, but RouteGate could not confirm that the service is running.',
    stoppedTitle: 'VPN service is stopped',
    stoppedDescription: 'Start sing-box before deploying or serving VPN configurations.',
    runningTitle: 'VPN service is running',
    runningDescription: 'sing-box is available and the system service is active.',
    failedTitle: 'VPN service needs attention',
    failedDescription: 'The sing-box service reported a failed state. Review technical details before retrying.',
    unknownTitle: 'VPN service state is unknown',
    unknownDescription: 'RouteGate could not determine the current sing-box service state.',
    installAction: 'Install VPN Core',
    startAction: 'Start VPN service',
    retryAction: 'Check again',
    updateAction: 'Update Agent',
    plannedAction: 'Management action coming next',
    version: 'Version',
    service: 'Service',
    serviceState: 'Service state',
    binaryPath: 'Binary path',
    checkedAt: 'Last checked',
    technicalDetails: 'Technical details',
    notAvailable: 'Not available',
  },
  ru: {
    title: 'VPN-служба',
    subtitle: 'RouteGate проверяет VPN Core на этом сервере и показывает следующее рекомендуемое действие.',
    connectedFirst: 'Сначала подключите сервер',
    connectedFirstDescription: 'RouteGate Agent должен быть подключён, прежде чем RouteGate сможет проверять VPN-службу и управлять ею.',
    legacyTitle: 'Обновите RouteGate Agent',
    legacyDescription: 'Эта версия Agent ещё не передаёт состояние VPN Core. Обновите её, чтобы включить управляемый сценарий VPN-службы.',
    notInstalledTitle: 'VPN Core не установлен',
    notInstalledDescription: 'Установите sing-box, чтобы подготовить сервер к развёртыванию VPN-конфигураций.',
    installedTitle: 'VPN Core установлен',
    installedDescription: 'sing-box доступен, но RouteGate не смог подтвердить, что служба запущена.',
    stoppedTitle: 'VPN-служба остановлена',
    stoppedDescription: 'Запустите sing-box перед развёртыванием и обслуживанием VPN-конфигураций.',
    runningTitle: 'VPN-служба работает',
    runningDescription: 'sing-box доступен, а системная служба активна.',
    failedTitle: 'VPN-служба требует внимания',
    failedDescription: 'Служба sing-box сообщила об ошибке. Перед повторной попыткой проверьте технические сведения.',
    unknownTitle: 'Состояние VPN-службы неизвестно',
    unknownDescription: 'RouteGate не смог определить текущее состояние службы sing-box.',
    installAction: 'Установить VPN Core',
    startAction: 'Запустить VPN-службу',
    retryAction: 'Проверить снова',
    updateAction: 'Обновить Agent',
    plannedAction: 'Управляющее действие будет добавлено следующим этапом',
    version: 'Версия',
    service: 'Служба',
    serviceState: 'Состояние службы',
    binaryPath: 'Путь к бинарному файлу',
    checkedAt: 'Последняя проверка',
    technicalDetails: 'Технические сведения',
    notAvailable: 'Нет данных',
  },
} as const;

function valueOrFallback(value: string | undefined, fallback: string): string {
  return value?.trim() ? value : fallback;
}

function formatDate(value: string | undefined, fallback: string): string {
  if (!value) {
    return fallback;
  }

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

export function ServerDetailsWithVPNCorePage() {
  const { serverId } = useParams<{ serverId: string }>();
  const locale = getCurrentLocale();
  const text = copy[locale];
  const serverQuery = useQuery({
    queryKey: ['server', serverId],
    queryFn: () => getServer(serverId ?? ''),
    enabled: Boolean(serverId),
    refetchInterval: 30_000,
  });

  const agent = serverQuery.data?.agent;
  const status = parseVPNCoreStatus(agent?.capabilities);

  let title = text.unknownTitle;
  let description = text.unknownDescription;
  let action = text.retryAction;
  let tone = 'unknown';

  if (!agent) {
    title = text.connectedFirst;
    description = text.connectedFirstDescription;
    action = text.connectedFirst;
    tone = 'offline';
  } else if (!status) {
    title = text.legacyTitle;
    description = text.legacyDescription;
    action = text.updateAction;
    tone = 'upgrade-recommended';
  } else {
    tone = status.state;
    switch (status.state) {
      case 'not_installed':
        title = text.notInstalledTitle;
        description = text.notInstalledDescription;
        action = text.installAction;
        break;
      case 'running':
        title = text.runningTitle;
        description = text.runningDescription;
        action = text.plannedAction;
        break;
      case 'stopped':
        title = text.stoppedTitle;
        description = text.stoppedDescription;
        action = text.startAction;
        break;
      case 'failed':
      case 'degraded':
        title = text.failedTitle;
        description = text.failedDescription;
        action = text.retryAction;
        break;
      case 'installed':
        title = text.installedTitle;
        description = text.installedDescription;
        action = text.startAction;
        break;
      default:
        break;
    }
  }

  return (
    <>
      <ServerDetailsPage />
      <section className="page server-details-page vpn-core-management-section">
        <div className="panel vpn-core-status-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">{text.title}</div>
              <p className="panel-subtitle">{text.subtitle}</p>
            </div>
            <span className={`badge badge-${tone.replace(/[^a-z0-9-]/g, '-')}`}>{title}</span>
          </div>

          <div className="empty-state empty-state-card">
            <strong>{title}</strong>
            <span>{description}</span>
            <button className="primary-button" type="button" disabled>
              {action}
            </button>
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
    </>
  );
}
