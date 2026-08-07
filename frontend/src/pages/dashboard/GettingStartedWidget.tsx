import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getManagerHealth } from '../../entities/health/api/healthApi';
import {
  getProtocolSettings,
  getServers,
  type ProtocolSettingsResponse,
} from '../../entities/server/api/serverApi';
import {
  createVPNCoreInstallation,
  getVPNCoreInstallation,
} from '../../entities/server/api/vpnCoreApi';
import {
  isVPNCoreOperational,
  parseVPNCoreStatus,
} from '../../entities/server/model/vpnCoreStatus';
import { getVpnAccounts } from '../../entities/vpnAccount/api/vpnAccountApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';
import './getting-started.css';

type SetupStepState = 'complete' | 'current' | 'pending';

type SetupStepText = {
  label: string;
  description: string;
};

type SetupStep = {
  key: string;
  complete: boolean;
  copy: Record<SetupStepState, SetupStepText>;
  to?: string | null;
};

const GETTING_STARTED_DISMISSED_KEY = 'routegate.gettingStarted.dismissed.v1';

function readGettingStartedDismissed(): boolean {
  if (typeof window === 'undefined') return false;
  try {
    return window.localStorage.getItem(GETTING_STARTED_DISMISSED_KEY) === '1';
  } catch {
    return false;
  }
}

function persistGettingStartedDismissed(dismissed: boolean): void {
  if (typeof window === 'undefined') return;
  try {
    if (dismissed) {
      window.localStorage.setItem(GETTING_STARTED_DISMISSED_KEY, '1');
    } else {
      window.localStorage.removeItem(GETTING_STARTED_DISMISSED_KEY);
    }
  } catch {
    // localStorage can be unavailable in restricted browser modes; onboarding still works in-memory.
  }
}

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

function supportsInstallation(capabilities?: Record<string, unknown>): boolean {
  const value = capabilities?.vpnCoreInstallationOperations;
  return Array.isArray(value) && value.includes('install_sing_box');
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() ? error.message : fallback;
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
      openStep: 'Открыть',
      installed: {
        complete: {
          label: 'RouteGate установлен',
          description: 'Manager и веб-интерфейс доступны.',
        },
        current: {
          label: 'Проверить RouteGate',
          description: 'Проверяем доступность Manager и веб-интерфейса.',
        },
        pending: {
          label: 'Проверить RouteGate',
          description: 'Сначала RouteGate должен подтвердить готовность Manager.',
        },
      },
      server: {
        complete: {
          label: 'Сервер подключён',
          description: 'Локальный сервер и Agent находятся онлайн.',
        },
        current: {
          label: 'Подключить сервер',
          description: 'Подключите локальный сервер и дождитесь, когда Agent станет онлайн.',
        },
        pending: {
          label: 'Подключить сервер',
          description: 'Этот шаг станет доступен после проверки RouteGate.',
        },
      },
      core: {
        complete: {
          label: 'VPN Core работает',
          description: 'sing-box установлен и запущен через RouteGate.',
        },
        current: {
          label: 'Установить VPN Core',
          description: 'RouteGate установит и запустит рекомендуемый VPN Core — sing-box.',
        },
        pending: {
          label: 'Установить VPN Core',
          description: 'Этот шаг станет доступен после подключения сервера.',
        },
      },
      coreInstalledCurrent: {
        label: 'Запустить VPN Core',
        description: 'sing-box уже установлен. Запустите его, чтобы продолжить настройку.',
      },
      protocol: {
        complete: {
          label: 'VLESS / Reality настроен',
          description: 'Протокол, Reality-ключ и SNI готовы.',
        },
        current: {
          label: 'Настроить VLESS / Reality',
          description: 'Задайте параметры VLESS, сгенерируйте Reality keypair и сохраните SNI.',
        },
        pending: {
          label: 'Настроить VLESS / Reality',
          description: 'Этот шаг станет доступен после запуска VPN Core.',
        },
      },
      account: {
        complete: {
          label: 'VPN-аккаунт создан',
          description: 'Первый активный аккаунт привязан к этому серверу.',
        },
        current: {
          label: 'Создать VPN-аккаунт',
          description: 'Создайте первый активный аккаунт и привяжите его к этому серверу.',
        },
        pending: {
          label: 'Создать VPN-аккаунт',
          description: 'Этот шаг станет доступен после настройки VLESS / Reality.',
        },
      },
      systemActionTitle: 'Проверить RouteGate',
      systemActionDescription: 'Manager пока не подтвердил готовность. Повторим проверку состояния.',
      addServerTitle: 'Подключить сервер',
      addServerDescription: 'RouteGate не видит подключённый Agent. Откройте сервер и завершите подключение.',
      addServerAction: 'Открыть серверы',
      installCoreTitle: 'Установить VPN Core',
      installCoreDescription: 'RouteGate установит sing-box через подключённый Agent и проверит результат.',
      manageCoreTitle: 'Запустить VPN Core',
      manageCoreDescription: 'sing-box установлен, но пока не находится в рабочем состоянии.',
      installCoreAction: 'Установить',
      installCorePending: 'Устанавливаем…',
      installCoreQueued: 'Установка выполняется через RouteGate Agent. Этот шаг обновится автоматически.',
      installCoreFailed: 'Не удалось установить VPN Core. Можно повторить попытку или открыть сервер для подробностей.',
      installCoreConfirm: (server: string) => `Установить VPN Core на ${server}?\n\nRouteGate установит sing-box и настроит его как системную службу.`,
      openCoreAction: 'Открыть VPN Core',
      protocolTitle: 'Настроить VLESS / Reality',
      protocolDescription: 'Задайте параметры VLESS, сгенерируйте Reality keypair и сохраните SNI.',
      protocolAction: 'Настроить протокол',
      accountTitle: 'Создать первый VPN-аккаунт',
      accountDescription: 'Создайте активный аккаунт и привяжите его к этому серверу.',
      accountAction: 'Создать VPN-аккаунт',
      readyTitle: 'VPN готов к подключению',
      readyDescription: 'Основная настройка завершена. Откройте аккаунт, покажите QR-код или скопируйте VLESS-ссылку на устройство.',
      readyAction: 'Открыть аккаунт и QR',
      readyCompactEyebrow: 'Настройка завершена',
      readyCompactTitle: 'RouteGate готов',
      readyCompactDescription: 'VPN Core работает, VLESS / Reality настроен, первый VPN-аккаунт создан.',
      readyCompactHint: 'Этот блок можно скрыть. Если обязательный шаг снова потребует внимания, RouteGate автоматически вернёт путеводитель.',
      dismiss: 'Скрыть',
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
    openStep: 'Open',
    installed: {
      complete: {
        label: 'RouteGate installed',
        description: 'Manager and the web interface are available.',
      },
      current: {
        label: 'Check RouteGate',
        description: 'Checking that Manager and the web interface are available.',
      },
      pending: {
        label: 'Check RouteGate',
        description: 'RouteGate must confirm Manager readiness first.',
      },
    },
    server: {
      complete: {
        label: 'Server connected',
        description: 'The local server and Agent are online.',
      },
      current: {
        label: 'Connect the server',
        description: 'Connect the local server and wait for its Agent to come online.',
      },
      pending: {
        label: 'Connect the server',
        description: 'This step becomes available after RouteGate is ready.',
      },
    },
    core: {
      complete: {
        label: 'VPN Core running',
        description: 'sing-box is installed and running through RouteGate.',
      },
      current: {
        label: 'Install VPN Core',
        description: 'RouteGate will install and start the recommended VPN Core — sing-box.',
      },
      pending: {
        label: 'Install VPN Core',
        description: 'This step becomes available after the server is connected.',
      },
    },
    coreInstalledCurrent: {
      label: 'Start VPN Core',
      description: 'sing-box is already installed. Start it to continue setup.',
    },
    protocol: {
      complete: {
        label: 'VLESS / Reality configured',
        description: 'Protocol settings, Reality key, and SNI are ready.',
      },
      current: {
        label: 'Configure VLESS / Reality',
        description: 'Set the VLESS parameters, generate the Reality keypair, and save the SNI.',
      },
      pending: {
        label: 'Configure VLESS / Reality',
        description: 'This step becomes available after VPN Core is running.',
      },
    },
    account: {
      complete: {
        label: 'VPN account created',
        description: 'The first active account is assigned to this server.',
      },
      current: {
        label: 'Create VPN account',
        description: 'Create the first active account and assign it to this server.',
      },
      pending: {
        label: 'Create VPN account',
        description: 'This step becomes available after VLESS / Reality is configured.',
      },
    },
    systemActionTitle: 'Check RouteGate',
    systemActionDescription: 'Manager has not confirmed readiness yet. Check the setup state again.',
    addServerTitle: 'Connect the server',
    addServerDescription: 'RouteGate does not see a connected Agent. Open Servers and finish the connection.',
    addServerAction: 'Open Servers',
    installCoreTitle: 'Install VPN Core',
    installCoreDescription: 'RouteGate will install sing-box through the connected Agent and verify the result.',
    manageCoreTitle: 'Start VPN Core',
    manageCoreDescription: 'sing-box is installed, but it is not currently operational.',
    installCoreAction: 'Install',
    installCorePending: 'Installing…',
    installCoreQueued: 'Installation is running through RouteGate Agent. This step will update automatically.',
    installCoreFailed: 'VPN Core installation failed. You can retry or open the server for details.',
    installCoreConfirm: (server: string) => `Install VPN Core on ${server}?\n\nRouteGate will install sing-box and configure it as a system service.`,
    openCoreAction: 'Open VPN Core',
    protocolTitle: 'Configure VLESS / Reality',
    protocolDescription: 'Set the VLESS parameters, generate the Reality keypair, and save the SNI.',
    protocolAction: 'Configure protocol',
    accountTitle: 'Create your first VPN account',
    accountDescription: 'Create an active account and assign it to this server.',
    accountAction: 'Create VPN account',
    readyTitle: 'VPN is ready to connect',
    readyDescription: 'Core setup is complete. Open the account to show its QR code or copy the VLESS link to a device.',
    readyAction: 'Open account and QR',
    readyCompactEyebrow: 'Setup complete',
    readyCompactTitle: 'RouteGate is ready',
    readyCompactDescription: 'VPN Core is running, VLESS / Reality is configured, and the first VPN account is ready.',
    readyCompactHint: 'You can hide this card. If a required step needs attention again, RouteGate will automatically bring the guide back.',
    dismiss: 'Hide',
  } as const;
}

export function GettingStartedWidget() {
  const copy = getCopy();
  const [installationJobId, setInstallationJobId] = useState<string | null>(null);
  const [installationFailure, setInstallationFailure] = useState<string | null>(null);
  const [dismissed, setDismissed] = useState(readGettingStartedDismissed);

  const managerHealthQuery = useQuery({
    queryKey: ['manager-health'],
    queryFn: getManagerHealth,
    refetchInterval: 10_000,
  });

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
    refetchInterval: installationJobId ? 2_000 : 10_000,
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
  const installationSupported = supportsInstallation(primaryServer?.agent?.capabilities);

  const installationMutation = useMutation({
    mutationFn: () => createVPNCoreInstallation(primaryServer?.id ?? ''),
    onSuccess: ({ job }) => {
      setInstallationFailure(null);
      setInstallationJobId(job.id);
      void serversQuery.refetch();
    },
    onError: (error) => {
      setInstallationFailure(errorMessage(error, copy.installCoreFailed));
    },
  });

  const installationQuery = useQuery({
    queryKey: ['vpn-core-installation', primaryServer?.id, installationJobId],
    queryFn: () => getVPNCoreInstallation(primaryServer?.id ?? '', installationJobId ?? ''),
    enabled: Boolean(primaryServer?.id && installationJobId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'failed' || status === 'succeeded' ? false : 2_000;
    },
  });

  const protocolQuery = useQuery({
    queryKey: ['server-protocol-settings', primaryServer?.id],
    queryFn: () => getProtocolSettings(primaryServer?.id ?? ''),
    enabled: Boolean(primaryServer?.id && vpnCoreReady),
    retry: false,
    refetchInterval: 10_000,
  });

  useEffect(() => {
    const job = installationQuery.data;
    if (!installationJobId || !job) return;

    if (job.status === 'failed') {
      setInstallationFailure(
        typeof job.errorMessage === 'string' && job.errorMessage.trim()
          ? job.errorMessage.trim()
          : copy.installCoreFailed,
      );
      setInstallationJobId(null);
      return;
    }

    if (job.status === 'succeeded') {
      void serversQuery.refetch();
      if (vpnCoreStatus?.installed) {
        setInstallationJobId(null);
      }
    }
  }, [installationJobId, installationQuery.data, vpnCoreStatus?.installed]);

  const managerReady = managerHealthQuery.isSuccess;
  const protocolReady = vpnCoreReady && protocolConfigured(protocolQuery.data);
  const firstReadyAccount = accountsQuery.data?.items.find((account) =>
    account.serverId === primaryServer?.id && account.status.trim().toLowerCase() === 'active',
  ) ?? null;
  const accountReady = protocolReady && firstReadyAccount !== null;
  const installationBusy = installationMutation.isPending || installationJobId !== null;
  const inlineInstallAvailable = Boolean(
    managerReady
      && serverConnected
      && primaryServer
      && vpnCoreStatus?.installed !== true
      && installationSupported,
  );

  const runInstallation = () => {
    if (!primaryServer || installationBusy || !inlineInstallAvailable) return;
    const serverName = primaryServer.name || primaryServer.id;
    if (!window.confirm(copy.installCoreConfirm(serverName))) return;

    installationMutation.reset();
    setInstallationFailure(null);
    installationMutation.mutate();
  };

  const coreCopy = vpnCoreStatus?.installed === true && !vpnCoreReady
    ? { ...copy.core, current: copy.coreInstalledCurrent }
    : copy.core;

  const steps: SetupStep[] = [
    { key: 'installed', copy: copy.installed, complete: managerReady },
    {
      key: 'server',
      copy: copy.server,
      complete: managerReady && serverConnected,
      to: primaryServer ? `/servers/${primaryServer.id}` : '/servers',
    },
    {
      key: 'core',
      copy: coreCopy,
      complete: managerReady && serverConnected && vpnCoreReady,
      to: primaryServer ? `/servers/${primaryServer.id}` : null,
    },
    {
      key: 'protocol',
      copy: copy.protocol,
      complete: managerReady && serverConnected && vpnCoreReady && protocolReady,
      to: primaryServer ? `/protocol-settings/${primaryServer.id}` : null,
    },
    {
      key: 'account',
      copy: copy.account,
      complete: managerReady && serverConnected && vpnCoreReady && protocolReady && accountReady,
      to: firstReadyAccount ? `/vpn-accounts/${firstReadyAccount.id}` : '/vpn-accounts?create=1',
    },
  ];

  const completedCount = steps.filter((step) => step.complete).length;
  const currentStepIndex = steps.findIndex((step) => !step.complete);
  const allReady = currentStepIndex === -1;
  const protocolLoading = Boolean(primaryServer?.id && vpnCoreReady) && protocolQuery.isPending;
  const loading = managerHealthQuery.isPending || serversQuery.isPending || accountsQuery.isPending || protocolLoading;
  const failed = managerHealthQuery.isError || serversQuery.isError || accountsQuery.isError
    || (Boolean(primaryServer?.id && vpnCoreReady) && protocolQuery.isError);

  useEffect(() => {
    if (!dismissed || loading || failed || allReady) return;
    persistGettingStartedDismissed(false);
    setDismissed(false);
  }, [allReady, dismissed, failed, loading]);

  const retry = () => {
    void managerHealthQuery.refetch();
    void serversQuery.refetch();
    void accountsQuery.refetch();
    if (primaryServer?.id && vpnCoreReady) {
      void protocolQuery.refetch();
    }
  };

  const dismiss = () => {
    persistGettingStartedDismissed(true);
    setDismissed(true);
  };

  let actionTitle: string = copy.systemActionTitle;
  let actionDescription: string = copy.systemActionDescription;
  let actionLabel: string = copy.retry;
  let actionTo: string | null = null;
  let actionInstall = false;

  if (managerReady && !serverConnected) {
    actionTitle = copy.addServerTitle;
    actionDescription = copy.addServerDescription;
    actionLabel = copy.addServerAction;
    actionTo = '/servers';
  } else if (managerReady && serverConnected && !vpnCoreReady && primaryServer) {
    const coreInstalled = vpnCoreStatus?.installed === true;
    actionTitle = coreInstalled ? copy.manageCoreTitle : copy.installCoreTitle;
    actionDescription = coreInstalled ? copy.manageCoreDescription : copy.installCoreDescription;

    if (!coreInstalled && installationSupported) {
      actionLabel = installationBusy ? copy.installCorePending : copy.installCoreAction;
      actionInstall = true;
    } else {
      actionLabel = copy.openCoreAction;
      actionTo = `/servers/${primaryServer.id}`;
    }
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

  if (!loading && !failed && allReady && dismissed) {
    return null;
  }

  if (!loading && !failed && allReady) {
    return (
      <section
        className="dashboard-widget getting-started-widget getting-started-widget-complete"
        aria-labelledby="getting-started-complete-title"
      >
        <div className="getting-started-complete-layout">
          <span className="getting-started-complete-check" aria-hidden="true">✓</span>
          <div className="getting-started-complete-copy">
            <span className="getting-started-eyebrow">{copy.readyCompactEyebrow}</span>
            <h2 id="getting-started-complete-title">{copy.readyCompactTitle}</h2>
            <p>{copy.readyCompactDescription}</p>
            <small>{copy.readyCompactHint}</small>
          </div>
          <div className="getting-started-complete-actions">
            {firstReadyAccount && (
              <Link className="getting-started-action" to={`/vpn-accounts/${firstReadyAccount.id}`}>
                {copy.readyAction} →
              </Link>
            )}
            <button className="getting-started-dismiss" type="button" onClick={dismiss}>
              {copy.dismiss} ×
            </button>
          </div>
        </div>
      </section>
    );
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
              const state: SetupStepState = step.complete ? 'complete' : isCurrent ? 'current' : 'pending';
              const stateLabel = step.complete ? copy.complete : isCurrent ? copy.current : copy.pending;
              const stepText = step.copy[state];
              const stepTo = state === 'pending' ? null : step.to ?? null;
              const showInlineInstall = step.key === 'core' && isCurrent && inlineInstallAvailable;
              const interactive = Boolean(stepTo || showInlineInstall);

              return (
                <li
                  className={`getting-started-step getting-started-step-${state}${interactive ? ' getting-started-step-interactive' : ''}`}
                  key={step.key}
                  aria-current={isCurrent ? 'step' : undefined}
                >
                  {stepTo && (
                    <Link
                      className="getting-started-step-overlay"
                      to={stepTo}
                      aria-label={`${copy.openStep}: ${stepText.label}`}
                    />
                  )}
                  <span className="getting-started-step-marker" aria-hidden="true">
                    {step.complete ? '✓' : index + 1}
                  </span>
                  <div className="getting-started-step-content">
                    <div className="getting-started-step-heading">
                      <strong>{stepText.label}</strong>
                      <small>{stateLabel}</small>
                    </div>
                    <p>{stepText.description}</p>
                    {showInlineInstall ? (
                      <button
                        className="getting-started-step-action"
                        type="button"
                        disabled={installationBusy}
                        onClick={runInstallation}
                      >
                        {installationBusy ? copy.installCorePending : `${copy.installCoreAction} →`}
                      </button>
                    ) : stepTo ? (
                      <span className="getting-started-step-open">{copy.openStep} →</span>
                    ) : null}
                    {showInlineInstall && installationBusy && (
                      <span className="getting-started-step-message">{copy.installCoreQueued}</span>
                    )}
                    {showInlineInstall && installationFailure && !installationBusy && (
                      <span className="getting-started-step-message getting-started-step-message-error">
                        {installationFailure}
                      </span>
                    )}
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
            {actionInstall && installationBusy && (
              <small className="getting-started-action-status">{copy.installCoreQueued}</small>
            )}
            {actionInstall && installationFailure && !installationBusy && (
              <small className="getting-started-action-status getting-started-action-status-error">
                {installationFailure}
              </small>
            )}
            {actionInstall ? (
              <button
                className="getting-started-action"
                type="button"
                disabled={installationBusy}
                onClick={runInstallation}
              >
                {installationBusy ? copy.installCorePending : actionLabel}
              </button>
            ) : actionTo ? (
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
