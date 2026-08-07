import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getManagerHealth } from '../../entities/health/api/healthApi';
import {
  getProtocolSettings,
  getServers,
  type ProtocolSettingsResponse,
} from '../../entities/server/api/serverApi';
import {
  isVPNCoreOperational,
  parseVPNCoreStatus,
} from '../../entities/server/model/vpnCoreStatus';
import { getVpnAccounts } from '../../entities/vpnAccount/api/vpnAccountApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';
import './getting-started.css';

type SetupStep = {
  key: string;
  label: string;
  description: string;
  complete: boolean;
};

function textPresent(value?: string | null): boolean {
  return typeof value === 'string' && value.trim() !== '';
}

function protocolConfigured(settings?: ProtocolSettingsResponse): boolean {
  if (!settings) return false;

  return settings.protocol.trim().toLowerCase() === 'vless'
    && settings.vless.port >= 1
    && settings.vless.port <= 65535
    && settings.reality.enabled
    && textPresent(settings.reality.publicKey)
    && textPresent(settings.reality.shortId)
    && textPresent(settings.reality.serverName);
}

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      eyebrow: 'Первоначальная настройка',
      title: 'Начало работы',
      subtitle: 'RouteGate ведёт вас к первому рабочему VPN. Выполняйте шаги по порядку — следующий нужный переход всегда показан справа.',
      checking: 'Проверяем состояние RouteGate…',
      checkFailed: 'Не удалось определить состояние первоначальной настройки.',
      retry: 'Проверить снова',
      progress: (done: number, total: number) => `${done} из ${total}`,
      complete: 'Готово',
      pending: 'Ожидает',
      current: 'Сейчас',
      nextAction: 'Следующий шаг',
      serverName: 'Сервер',
      installed: {
        label: 'RouteGate установлен',
        description: 'Manager и веб-интерфейс доступны.',
      },
      server: {
        label: 'Сервер подключён',
        description: 'Локальный сервер и Agent должны быть онлайн.',
      },
      core: {
        label: 'VPN Core работает',
        description: 'sing-box установлен и запущен через RouteGate.',
      },
      protocol: {
        label: 'VLESS / Reality настроен',
        description: 'Протокол, Reality-ключ и SNI готовы.',
      },
      account: {
        label: 'VPN-аккаунт создан',
        description: 'Первый активный аккаунт привязан к этому серверу.',
      },
      systemActionTitle: 'Проверить RouteGate',
      systemActionDescription: 'Manager пока не подтвердил готовность. Повторим проверку состояния.',
      addServerTitle: 'Подключить сервер',
      addServerDescription: 'RouteGate не видит подключённый Agent. Откройте сервер и завершите подключение.',
      addServerAction: 'Открыть серверы',
      installCoreTitle: 'Установить VPN Core',
      installCoreDescription: 'Установите и запустите sing-box на подключённом сервере.',
      manageCoreTitle: 'Запустить VPN Core',
      manageCoreDescription: 'sing-box установлен, но пока не находится в рабочем состоянии.',
      installCoreAction: 'Открыть VPN Core',
      protocolTitle: 'Настроить VLESS / Reality',
      protocolDescription: 'Задайте параметры VLESS, сгенерируйте Reality keypair и сохраните SNI.',
      protocolAction: 'Настроить протокол',
      accountTitle: 'Создать первый VPN-аккаунт',
      accountDescription: 'Создайте активный аккаунт и привяжите его к этому серверу.',
      accountAction: 'Создать VPN-аккаунт',
      readyTitle: 'VPN готов к подключению',
      readyDescription: 'Основная настройка завершена. Откройте аккаунт, покажите QR-код или скопируйте VLESS-ссылку на устройство.',
      readyAction: 'Открыть аккаунт и QR',
    } as const;
  }

  return {
    eyebrow: 'First-run setup',
    title: 'Getting started',
    subtitle: 'RouteGate guides you to your first working VPN. Complete the steps in order — the next required action is always shown on the right.',
    checking: 'Checking RouteGate setup state…',
    checkFailed: 'RouteGate could not determine the first-run setup state.',
    retry: 'Check again',
    progress: (done: number, total: number) => `${done} of ${total}`,
    complete: 'Complete',
    pending: 'Pending',
    current: 'Now',
    nextAction: 'Next action',
    serverName: 'Server',
    installed: {
      label: 'RouteGate installed',
      description: 'Manager and the web interface are available.',
    },
    server: {
      label: 'Server connected',
      description: 'The local server and Agent must be online.',
    },
    core: {
      label: 'VPN Core running',
      description: 'sing-box is installed and running through RouteGate.',
    },
    protocol: {
      label: 'VLESS / Reality configured',
      description: 'Protocol settings, Reality key, and SNI are ready.',
    },
    account: {
      label: 'VPN account created',
      description: 'The first active account is assigned to this server.',
    },
    systemActionTitle: 'Check RouteGate',
    systemActionDescription: 'Manager has not confirmed readiness yet. Check the setup state again.',
    addServerTitle: 'Connect the server',
    addServerDescription: 'RouteGate does not see a connected Agent. Open Servers and finish the connection.',
    addServerAction: 'Open Servers',
    installCoreTitle: 'Install VPN Core',
    installCoreDescription: 'Install and start sing-box on the connected server.',
    manageCoreTitle: 'Start VPN Core',
    manageCoreDescription: 'sing-box is installed, but it is not currently operational.',
    installCoreAction: 'Open VPN Core',
    protocolTitle: 'Configure VLESS / Reality',
    protocolDescription: 'Set the VLESS parameters, generate the Reality keypair, and save the SNI.',
    protocolAction: 'Configure protocol',
    accountTitle: 'Create your first VPN account',
    accountDescription: 'Create an active account and assign it to this server.',
    accountAction: 'Create VPN account',
    readyTitle: 'VPN is ready to connect',
    readyDescription: 'Core setup is complete. Open the account to show its QR code or copy the VLESS link to a device.',
    readyAction: 'Open account and QR',
  } as const;
}

export function GettingStartedWidget() {
  const copy = getCopy();

  const managerHealthQuery = useQuery({
    queryKey: ['manager-health'],
    queryFn: getManagerHealth,
    refetchInterval: 10_000,
  });

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
    refetchInterval: 10_000,
  });

  const accountsQuery = useQuery({
    queryKey: ['vpn-accounts'],
    queryFn: getVpnAccounts,
    refetchInterval: 10_000,
  });

  const servers = serversQuery.data?.items ?? [];
  const primaryServer = servers.find((server) => server.agent?.status === 'online') ?? servers[0] ?? null;
  const serverConnected = primaryServer?.agent?.status === 'online';
  const vpnCoreStatus = parseVPNCoreStatus(primaryServer?.agent?.capabilities);
  const vpnCoreReady = serverConnected && isVPNCoreOperational(vpnCoreStatus);

  const protocolQuery = useQuery({
    queryKey: ['server-protocol-settings', primaryServer?.id],
    queryFn: () => getProtocolSettings(primaryServer?.id ?? ''),
    enabled: Boolean(primaryServer?.id && vpnCoreReady),
    retry: false,
    refetchInterval: 10_000,
  });

  const managerReady = managerHealthQuery.isSuccess;
  const protocolReady = vpnCoreReady && protocolConfigured(protocolQuery.data);
  const firstReadyAccount = accountsQuery.data?.items.find((account) =>
    account.serverId === primaryServer?.id && account.status.trim().toLowerCase() === 'active',
  ) ?? null;
  const accountReady = protocolReady && firstReadyAccount !== null;

  const steps: SetupStep[] = [
    { key: 'installed', ...copy.installed, complete: managerReady },
    { key: 'server', ...copy.server, complete: managerReady && serverConnected },
    { key: 'core', ...copy.core, complete: managerReady && serverConnected && vpnCoreReady },
    { key: 'protocol', ...copy.protocol, complete: managerReady && serverConnected && vpnCoreReady && protocolReady },
    { key: 'account', ...copy.account, complete: managerReady && serverConnected && vpnCoreReady && protocolReady && accountReady },
  ];

  const completedCount = steps.filter((step) => step.complete).length;
  const currentStepIndex = steps.findIndex((step) => !step.complete);
  const allReady = currentStepIndex === -1;
  const protocolLoading = Boolean(primaryServer?.id && vpnCoreReady) && protocolQuery.isPending;
  const loading = managerHealthQuery.isPending || serversQuery.isPending || accountsQuery.isPending || protocolLoading;
  const failed = managerHealthQuery.isError || serversQuery.isError || accountsQuery.isError
    || (Boolean(primaryServer?.id && vpnCoreReady) && protocolQuery.isError);

  const retry = () => {
    void managerHealthQuery.refetch();
    void serversQuery.refetch();
    void accountsQuery.refetch();
    if (primaryServer?.id && vpnCoreReady) {
      void protocolQuery.refetch();
    }
  };

  let actionTitle = copy.systemActionTitle;
  let actionDescription = copy.systemActionDescription;
  let actionLabel = copy.retry;
  let actionTo: string | null = null;

  if (managerReady && !serverConnected) {
    actionTitle = copy.addServerTitle;
    actionDescription = copy.addServerDescription;
    actionLabel = copy.addServerAction;
    actionTo = '/servers';
  } else if (managerReady && serverConnected && !vpnCoreReady && primaryServer) {
    const coreInstalled = vpnCoreStatus?.installed === true;
    actionTitle = coreInstalled ? copy.manageCoreTitle : copy.installCoreTitle;
    actionDescription = coreInstalled ? copy.manageCoreDescription : copy.installCoreDescription;
    actionLabel = copy.installCoreAction;
    actionTo = `/servers/${primaryServer.id}`;
  } else if (managerReady && serverConnected && vpnCoreReady && !protocolReady && primaryServer) {
    actionTitle = copy.protocolTitle;
    actionDescription = copy.protocolDescription;
    actionLabel = copy.protocolAction;
    actionTo = `/protocol-settings/${primaryServer.id}`;
  } else if (managerReady && serverConnected && vpnCoreReady && protocolReady && !accountReady) {
    actionTitle = copy.accountTitle;
    actionDescription = copy.accountDescription;
    actionLabel = copy.accountAction;
    actionTo = '/vpn-accounts?create=1';
  } else if (allReady && firstReadyAccount) {
    actionTitle = copy.readyTitle;
    actionDescription = copy.readyDescription;
    actionLabel = copy.readyAction;
    actionTo = `/vpn-accounts/${firstReadyAccount.id}`;
  }

  return (
    <section className="dashboard-widget getting-started-widget" aria-labelledby="getting-started-title">
      <div className="getting-started-header">
        <div>
          <span className="getting-started-eyebrow">{copy.eyebrow}</span>
          <h2 id="getting-started-title">{copy.title}</h2>
          <p>{copy.subtitle}</p>
        </div>
        <div className="getting-started-progress-summary">
          <strong>{copy.progress(completedCount, steps.length)}</strong>
          <span>{allReady ? copy.complete : copy.current}</span>
        </div>
      </div>

      <div className="getting-started-progress" aria-hidden="true">
        <span style={{ width: `${(completedCount / steps.length) * 100}%` }} />
      </div>

      {loading ? (
        <div className="getting-started-status">{copy.checking}</div>
      ) : failed ? (
        <div className="getting-started-status getting-started-status-error">
          <span>{copy.checkFailed}</span>
          <button className="secondary-button" type="button" onClick={retry}>{copy.retry}</button>
        </div>
      ) : (
        <div className="getting-started-body">
          <ol className="getting-started-steps">
            {steps.map((step, index) => {
              const isCurrent = index === currentStepIndex;
              const state = step.complete ? 'complete' : isCurrent ? 'current' : 'pending';
              const stateLabel = step.complete ? copy.complete : isCurrent ? copy.current : copy.pending;

              return (
                <li
                  className={`getting-started-step getting-started-step-${state}`}
                  key={step.key}
                  aria-current={isCurrent ? 'step' : undefined}
                >
                  <span className="getting-started-step-marker" aria-hidden="true">
                    {step.complete ? '✓' : index + 1}
                  </span>
                  <div>
                    <div className="getting-started-step-heading">
                      <strong>{step.label}</strong>
                      <small>{stateLabel}</small>
                    </div>
                    <p>{step.description}</p>
                  </div>
                </li>
              );
            })}
          </ol>

          <aside className={`getting-started-next-action${allReady ? ' getting-started-next-action-ready' : ''}`}>
            <span>{copy.nextAction}</span>
            <h3>{actionTitle}</h3>
            <p>{actionDescription}</p>
            {primaryServer && (
              <small>{copy.serverName}: <strong>{primaryServer.name || primaryServer.id}</strong></small>
            )}
            {actionTo ? (
              <Link className="getting-started-action" to={actionTo}>{actionLabel} →</Link>
            ) : (
              <button className="getting-started-action" type="button" onClick={retry}>{actionLabel}</button>
            )}
          </aside>
        </div>
      )}
    </section>
  );
}
