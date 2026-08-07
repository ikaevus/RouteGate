import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getManagerHealth } from '../../entities/health/api/healthApi';
import {
  applyConfigVersion,
  getConfigApplyJobs,
  getConfigVersions,
  getProtocolSettings,
  getServers,
  renderConfig,
  validateConfigVersion,
  type ConfigVersion,
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

const dismissedStorageKey = 'routegate.gettingStarted.dismissed';

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

function timestamp(value?: string | null): number {
  if (!value) return 0;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? 0 : parsed;
}

function latestAppliedVersion(versions: ConfigVersion[]): ConfigVersion | null {
  return [...versions]
    .filter((version) => Boolean(version.appliedAt) || version.status.trim().toLowerCase() === 'applied')
    .sort((left, right) => timestamp(right.appliedAt ?? right.createdAt) - timestamp(left.appliedAt ?? left.createdAt))[0] ?? null;
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
        complete: { label: 'RouteGate установлен', description: 'Manager и веб-интерфейс доступны.' },
        current: { label: 'Проверить RouteGate', description: 'Проверяем доступность Manager и веб-интерфейса.' },
        pending: { label: 'Проверить RouteGate', description: 'Сначала RouteGate должен подтвердить готовность Manager.' },
      },
      server: {
        complete: { label: 'Сервер подключён', description: 'Локальный сервер и Agent находятся онлайн.' },
        current: { label: 'Подключить сервер', description: 'Подключите локальный сервер и дождитесь, когда Agent станет онлайн.' },
        pending: { label: 'Подключить сервер', description: 'Этот шаг станет доступен после проверки RouteGate.' },
      },
      core: {
        complete: { label: 'VPN Core установлен', description: 'sing-box установлен. RouteGate запустит его после развёртывания рабочего конфига.' },
        current: { label: 'Установить VPN Core', description: 'RouteGate установит рекомендуемый VPN Core — sing-box.' },
        pending: { label: 'Установить VPN Core', description: 'Этот шаг станет доступен после подключения сервера.' },
      },
      protocol: {
        complete: { label: 'VLESS / Reality настроен', description: 'Протокол, Reality-ключ и SNI готовы.' },
        current: { label: 'Настроить VLESS / Reality', description: 'RouteGate автоматически выберет рекомендуемые параметры и создаст Reality-ключи.' },
        pending: { label: 'Настроить VLESS / Reality', description: 'Этот шаг станет доступен после установки VPN Core.' },
      },
      final: {
        complete: { label: 'VPN готов', description: 'Аккаунт создан, конфигурация применена, sing-box работает.' },
        current: { label: 'Создать VPN-аккаунт', description: 'Создайте первый активный аккаунт и привяжите его к этому серверу.' },
        pending: { label: 'Создать VPN-аккаунт', description: 'Этот шаг станет доступен после настройки VLESS / Reality.' },
      },
      deployCurrent: {
        label: 'Развернуть VPN-конфигурацию',
        description: 'RouteGate отрендерит, проверит и применит конфигурацию, затем запустит sing-box.',
      },
      systemActionTitle: 'Проверить RouteGate',
      systemActionDescription: 'Manager пока не подтвердил готовность. Повторим проверку состояния.',
      addServerTitle: 'Подключить сервер',
      addServerDescription: 'RouteGate не видит подключённый Agent. Откройте сервер и завершите подключение.',
      addServerAction: 'Открыть серверы',
      installCoreTitle: 'Установить VPN Core',
      installCoreDescription: 'RouteGate установит sing-box через подключённый Agent и проверит результат.',
      installCoreAction: 'Установить',
      installCorePending: 'Устанавливаем…',
      installCoreQueued: 'Установка выполняется через RouteGate Agent. Этот шаг обновится автоматически.',
      installCoreFailed: 'Не удалось установить VPN Core. Можно повторить попытку или открыть сервер для подробностей.',
      installCoreConfirm: (server: string) => `Установить VPN Core на ${server}?\n\nRouteGate установит sing-box. Сервис будет запущен позже, после создания и применения рабочего VPN-конфига.`,
      openCoreAction: 'Открыть VPN Core',
      protocolTitle: 'Настроить VLESS / Reality',
      protocolDescription: 'RouteGate настроит рекомендуемые параметры VLESS / Reality для этого сервера.',
      protocolAction: 'Настроить протокол',
      accountTitle: 'Создать первый VPN-аккаунт',
      accountDescription: 'Создайте активный аккаунт и привяжите его к этому серверу.',
      accountAction: 'Создать VPN-аккаунт',
      deployTitle: 'Развернуть VPN-конфигурацию',
      deployDescription: 'RouteGate автоматически отрендерит, проверит и применит конфигурацию. Agent перезапустит sing-box и проверит его состояние.',
      deployAction: 'Развернуть VPN',
      deployPending: 'Развёртываем…',
      deployQueued: 'Конфигурация применяется через RouteGate Agent. Состояние обновится автоматически.',
      deployFailed: 'Не удалось развернуть VPN-конфигурацию. Можно повторить попытку или открыть сервер для подробностей.',
      deployValidationFailed: 'Сгенерированная VPN-конфигурация не прошла проверку.',
      deployConfirm: (server: string) => `Развернуть VPN-конфигурацию на ${server}?\n\nRouteGate отрендерит, проверит и применит конфиг. После применения Agent запустит или перезапустит sing-box и выполнит healthcheck.`,
      readyTitle: 'RouteGate готов',
      readyDescription: 'VPN Core работает, VLESS / Reality настроен и первый VPN-аккаунт готов к подключению.',
      readyAction: 'Открыть аккаунт и QR',
      dismiss: 'Скрыть',
      readyServer: (server: string) => `Рабочий сервер: ${server}`,
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
      complete: { label: 'RouteGate installed', description: 'Manager and the web interface are available.' },
      current: { label: 'Check RouteGate', description: 'Checking that Manager and the web interface are available.' },
      pending: { label: 'Check RouteGate', description: 'RouteGate must confirm Manager readiness first.' },
    },
    server: {
      complete: { label: 'Server connected', description: 'The local server and Agent are online.' },
      current: { label: 'Connect the server', description: 'Connect the local server and wait for its Agent to come online.' },
      pending: { label: 'Connect the server', description: 'This step becomes available after RouteGate is ready.' },
    },
    core: {
      complete: { label: 'VPN Core installed', description: 'sing-box is installed. RouteGate will start it after a working VPN config is deployed.' },
      current: { label: 'Install VPN Core', description: 'RouteGate will install the recommended VPN Core — sing-box.' },
      pending: { label: 'Install VPN Core', description: 'This step becomes available after the server is connected.' },
    },
    protocol: {
      complete: { label: 'VLESS / Reality configured', description: 'Protocol settings, Reality key, and SNI are ready.' },
      current: { label: 'Configure VLESS / Reality', description: 'RouteGate will automatically choose the recommended settings and generate the Reality keys.' },
      pending: { label: 'Configure VLESS / Reality', description: 'This step becomes available after VPN Core is installed.' },
    },
    final: {
      complete: { label: 'VPN ready', description: 'Account created, configuration applied, and sing-box is running.' },
      current: { label: 'Create VPN account', description: 'Create the first active account and assign it to this server.' },
      pending: { label: 'Create VPN account', description: 'This step becomes available after VLESS / Reality is configured.' },
    },
    deployCurrent: {
      label: 'Deploy VPN configuration',
      description: 'RouteGate will render, validate, and apply the configuration, then start sing-box.',
    },
    systemActionTitle: 'Check RouteGate',
    systemActionDescription: 'Manager has not confirmed readiness yet. Check the setup state again.',
    addServerTitle: 'Connect the server',
    addServerDescription: 'RouteGate does not see a connected Agent. Open Servers and finish the connection.',
    addServerAction: 'Open Servers',
    installCoreTitle: 'Install VPN Core',
    installCoreDescription: 'RouteGate will install sing-box through the connected Agent and verify the result.',
    installCoreAction: 'Install',
    installCorePending: 'Installing…',
    installCoreQueued: 'Installation is running through RouteGate Agent. This step will update automatically.',
    installCoreFailed: 'VPN Core installation failed. You can retry or open the server for details.',
    installCoreConfirm: (server: string) => `Install VPN Core on ${server}?\n\nRouteGate will install sing-box. The service will be started later, after a working VPN configuration is created and applied.`,
    openCoreAction: 'Open VPN Core',
    protocolTitle: 'Configure VLESS / Reality',
    protocolDescription: 'RouteGate will configure the recommended VLESS / Reality settings for this server.',
    protocolAction: 'Configure protocol',
    accountTitle: 'Create your first VPN account',
    accountDescription: 'Create an active account and assign it to this server.',
    accountAction: 'Create VPN account',
    deployTitle: 'Deploy VPN configuration',
    deployDescription: 'RouteGate will automatically render, validate, and apply the configuration. Agent will restart sing-box and verify its health.',
    deployAction: 'Deploy VPN',
    deployPending: 'Deploying…',
    deployQueued: 'Configuration is being applied through RouteGate Agent. This state will update automatically.',
    deployFailed: 'VPN configuration deployment failed. You can retry or open the server for details.',
    deployValidationFailed: 'The generated VPN configuration did not pass validation.',
    deployConfirm: (server: string) => `Deploy VPN configuration to ${server}?\n\nRouteGate will render, validate, and apply the config. After applying it, Agent will start or restart sing-box and run a healthcheck.`,
    readyTitle: 'RouteGate is ready',
    readyDescription: 'VPN Core is running, VLESS / Reality is configured, and the first VPN account is ready to connect.',
    readyAction: 'Open account and QR',
    dismiss: 'Hide',
    readyServer: (server: string) => `Working server: ${server}`,
  } as const;
}

export function GettingStartedWidget() {
  const copy = getCopy();
  const [installationJobId, setInstallationJobId] = useState<string | null>(null);
  const [installationFailure, setInstallationFailure] = useState<string | null>(null);
  const [deployJobId, setDeployJobId] = useState<string | null>(null);
  const [deployFailure, setDeployFailure] = useState<string | null>(null);
  const [dismissed, setDismissed] = useState(() => {
    try {
      return window.localStorage.getItem(dismissedStorageKey) === 'true';
    } catch {
      return false;
    }
  });

  const managerHealthQuery = useQuery({
    queryKey: ['manager-health'],
    queryFn: getManagerHealth,
    refetchInterval: 10_000,
  });

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
    refetchInterval: installationJobId || deployJobId ? 2_000 : 10_000,
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
  const vpnCoreInstalled = Boolean(serverConnected && vpnCoreStatus?.installed);
  const vpnCoreReady = Boolean(serverConnected && isVPNCoreOperational(vpnCoreStatus));
  const installationSupported = supportsInstallation(primaryServer?.agent?.capabilities);

  const protocolQuery = useQuery({
    queryKey: ['server-protocol-settings', primaryServer?.id],
    queryFn: () => getProtocolSettings(primaryServer?.id ?? ''),
    enabled: Boolean(primaryServer?.id && vpnCoreInstalled),
    retry: false,
    refetchInterval: 10_000,
  });

  const managerReady = managerHealthQuery.isSuccess;
  const protocolReady = vpnCoreInstalled && protocolConfigured(protocolQuery.data);
  const firstReadyAccount = accountsQuery.data?.items.find((account) =>
    account.serverId === primaryServer?.id && account.status.trim().toLowerCase() === 'active',
  ) ?? null;
  const accountReady = protocolReady && firstReadyAccount !== null;

  const configVersionsQuery = useQuery({
    queryKey: ['server-config-versions', primaryServer?.id],
    queryFn: () => getConfigVersions(primaryServer?.id ?? ''),
    enabled: Boolean(primaryServer?.id && accountReady),
    refetchInterval: deployJobId ? 2_000 : 10_000,
  });

  const applyJobsQuery = useQuery({
    queryKey: ['server-config-apply-jobs', primaryServer?.id],
    queryFn: () => getConfigApplyJobs(primaryServer?.id ?? ''),
    enabled: Boolean(primaryServer?.id && accountReady),
    refetchInterval: deployJobId ? 2_000 : 10_000,
  });

  const latestApplied = latestAppliedVersion(configVersionsQuery.data?.items ?? []);
  const sourceUpdatedAt = Math.max(
    timestamp(protocolQuery.data?.updatedAt),
    timestamp(firstReadyAccount?.updatedAt),
  );
  const appliedConfigIsCurrent = Boolean(
    latestApplied
      && timestamp(latestApplied.createdAt) >= sourceUpdatedAt
      && sourceUpdatedAt > 0,
  );
  const runtimeReady = Boolean(accountReady && appliedConfigIsCurrent && vpnCoreReady);

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

  const deploymentMutation = useMutation({
    mutationFn: async () => {
      const serverId = primaryServer?.id ?? '';
      const rendered = await renderConfig(serverId);
      if (!rendered.validationResult.valid) {
        throw new Error(copy.deployValidationFailed);
      }
      const validated = await validateConfigVersion(serverId, rendered.configVersion.id);
      if (!validated.validationResult.valid) {
        throw new Error(copy.deployValidationFailed);
      }
      return applyConfigVersion(serverId, rendered.configVersion.id);
    },
    onSuccess: ({ job }) => {
      setDeployFailure(null);
      setDeployJobId(job.id);
      void configVersionsQuery.refetch();
      void applyJobsQuery.refetch();
      void serversQuery.refetch();
    },
    onError: (error) => {
      setDeployFailure(errorMessage(error, copy.deployFailed));
    },
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

  const activeDeployJob = deployJobId
    ? applyJobsQuery.data?.items.find((job) => job.id === deployJobId) ?? null
    : null;

  useEffect(() => {
    if (!deployJobId || !activeDeployJob) return;

    if (activeDeployJob.status === 'failed') {
      setDeployFailure(activeDeployJob.errorMessage?.trim() || copy.deployFailed);
      setDeployJobId(null);
      return;
    }

    if (activeDeployJob.status === 'succeeded') {
      void configVersionsQuery.refetch();
      void serversQuery.refetch();
      if (runtimeReady) {
        setDeployJobId(null);
      }
    }
  }, [activeDeployJob, deployJobId, runtimeReady]);

  const installationBusy = installationMutation.isPending || installationJobId !== null;
  const deploymentBusy = deploymentMutation.isPending || deployJobId !== null;
  const inlineInstallAvailable = Boolean(
    managerReady
      && serverConnected
      && primaryServer
      && !vpnCoreInstalled
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

  const runDeployment = () => {
    if (!primaryServer || !accountReady || deploymentBusy) return;
    const serverName = primaryServer.name || primaryServer.id;
    if (!window.confirm(copy.deployConfirm(serverName))) return;

    deploymentMutation.reset();
    setDeployFailure(null);
    deploymentMutation.mutate();
  };

  const finalCopy = accountReady && !runtimeReady
    ? { ...copy.final, current: copy.deployCurrent }
    : copy.final;

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
      copy: copy.core,
      complete: managerReady && serverConnected && vpnCoreInstalled,
      to: primaryServer ? `/servers/${primaryServer.id}` : null,
    },
    {
      key: 'protocol',
      copy: copy.protocol,
      complete: managerReady && serverConnected && vpnCoreInstalled && protocolReady,
      to: primaryServer ? `/protocol-settings/${primaryServer.id}` : null,
    },
    {
      key: 'final',
      copy: finalCopy,
      complete: managerReady && serverConnected && vpnCoreInstalled && protocolReady && runtimeReady,
      to: runtimeReady && firstReadyAccount
        ? `/vpn-accounts/${firstReadyAccount.id}`
        : accountReady && primaryServer
          ? `/servers/${primaryServer.id}`
          : '/vpn-accounts?create=1',
    },
  ];

  const completedCount = steps.filter((step) => step.complete).length;
  const currentStepIndex = steps.findIndex((step) => !step.complete);
  const allReady = currentStepIndex === -1;
  const protocolLoading = Boolean(primaryServer?.id && vpnCoreInstalled) && protocolQuery.isPending;
  const configLoading = Boolean(primaryServer?.id && accountReady)
    && (configVersionsQuery.isPending || applyJobsQuery.isPending);
  const loading = managerHealthQuery.isPending
    || serversQuery.isPending
    || accountsQuery.isPending
    || protocolLoading
    || configLoading;
  const failed = managerHealthQuery.isError
    || serversQuery.isError
    || accountsQuery.isError
    || (Boolean(primaryServer?.id && vpnCoreInstalled) && protocolQuery.isError)
    || (Boolean(primaryServer?.id && accountReady) && (configVersionsQuery.isError || applyJobsQuery.isError));

  useEffect(() => {
    if (!allReady && dismissed) {
      setDismissed(false);
      try {
        window.localStorage.removeItem(dismissedStorageKey);
      } catch {
        // Local persistence is optional; state-aware restoration still wins in-memory.
      }
    }
  }, [allReady, dismissed]);

  const dismiss = () => {
    if (!allReady) return;
    setDismissed(true);
    try {
      window.localStorage.setItem(dismissedStorageKey, 'true');
    } catch {
      // Hiding the guide for this session is still useful when storage is unavailable.
    }
  };

  const retry = () => {
    void managerHealthQuery.refetch();
    void serversQuery.refetch();
    void accountsQuery.refetch();
    if (primaryServer?.id && vpnCoreInstalled) {
      void protocolQuery.refetch();
    }
    if (primaryServer?.id && accountReady) {
      void configVersionsQuery.refetch();
      void applyJobsQuery.refetch();
    }
  };

  if (allReady && dismissed) {
    return null;
  }

  if (allReady && firstReadyAccount) {
    return (
      <section className="dashboard-widget getting-started-widget getting-started-widget-complete" aria-labelledby="getting-started-complete-title">
        <div className="getting-started-complete-layout">
          <span className="getting-started-complete-check" aria-hidden="true">✓</span>
          <div className="getting-started-complete-copy">
            <span className="getting-started-eyebrow">{copy.eyebrow}</span>
            <h2 id="getting-started-complete-title">{copy.readyTitle}</h2>
            <p>{copy.readyDescription}</p>
            {primaryServer && <small>{copy.readyServer(primaryServer.name || primaryServer.id)}</small>}
          </div>
          <div className="getting-started-complete-actions">
            <Link className="getting-started-action" to={`/vpn-accounts/${firstReadyAccount.id}`}>
              {copy.readyAction} →
            </Link>
            <button className="getting-started-dismiss" type="button" onClick={dismiss}>
              {copy.dismiss} ×
            </button>
          </div>
        </div>
      </section>
    );
  }

  let actionTitle: string = copy.systemActionTitle;
  let actionDescription: string = copy.systemActionDescription;
  let actionLabel: string = copy.retry;
  let actionTo: string | null = null;
  let actionInstall = false;
  let actionDeploy = false;

  if (managerReady && !serverConnected) {
    actionTitle = copy.addServerTitle;
    actionDescription = copy.addServerDescription;
    actionLabel = copy.addServerAction;
    actionTo = '/servers';
  } else if (managerReady && serverConnected && !vpnCoreInstalled && primaryServer) {
    actionTitle = copy.installCoreTitle;
    actionDescription = copy.installCoreDescription;
    if (installationSupported) {
      actionLabel = installationBusy ? copy.installCorePending : copy.installCoreAction;
      actionInstall = true;
    } else {
      actionLabel = copy.openCoreAction;
      actionTo = `/servers/${primaryServer.id}`;
    }
  } else if (managerReady && serverConnected && vpnCoreInstalled && !protocolReady && primaryServer) {
    actionTitle = copy.protocolTitle;
    actionDescription = copy.protocolDescription;
    actionLabel = copy.protocolAction;
    actionTo = `/protocol-settings/${primaryServer.id}`;
  } else if (managerReady && serverConnected && vpnCoreInstalled && protocolReady && !accountReady) {
    actionTitle = copy.accountTitle;
    actionDescription = copy.accountDescription;
    actionLabel = copy.accountAction;
    actionTo = '/vpn-accounts?create=1';
  } else if (accountReady && !runtimeReady && primaryServer) {
    actionTitle = copy.deployTitle;
    actionDescription = copy.deployDescription;
    actionLabel = deploymentBusy ? copy.deployPending : copy.deployAction;
    actionDeploy = true;
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
          <span>{copy.current}</span>
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
              const showInlineInstall = step.key === 'core' && isCurrent && inlineInstallAvailable;
              const showInlineDeploy = step.key === 'final' && isCurrent && accountReady;
              const stepTo = state === 'pending' || showInlineInstall || showInlineDeploy ? null : step.to ?? null;
              const interactive = Boolean(stepTo || showInlineInstall || showInlineDeploy);

              return (
                <li
                  className={`getting-started-step getting-started-step-${state}${interactive ? ' getting-started-step-interactive' : ''}`}
                  key={step.key}
                  aria-current={isCurrent ? 'step' : undefined}
                >
                  {stepTo && (
                    <Link className="getting-started-step-overlay" to={stepTo} aria-label={`${copy.openStep}: ${stepText.label}`} />
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
                      <button className="getting-started-step-action" type="button" disabled={installationBusy} onClick={runInstallation}>
                        {installationBusy ? copy.installCorePending : `${copy.installCoreAction} →`}
                      </button>
                    ) : showInlineDeploy ? (
                      <button className="getting-started-step-action" type="button" disabled={deploymentBusy} onClick={runDeployment}>
                        {deploymentBusy ? copy.deployPending : `${copy.deployAction} →`}
                      </button>
                    ) : stepTo ? (
                      <span className="getting-started-step-open">{copy.openStep} →</span>
                    ) : null}
                    {showInlineInstall && installationBusy && <span className="getting-started-step-message">{copy.installCoreQueued}</span>}
                    {showInlineInstall && installationFailure && !installationBusy && (
                      <span className="getting-started-step-message getting-started-step-message-error">{installationFailure}</span>
                    )}
                    {showInlineDeploy && deploymentBusy && <span className="getting-started-step-message">{copy.deployQueued}</span>}
                    {showInlineDeploy && deployFailure && !deploymentBusy && (
                      <span className="getting-started-step-message getting-started-step-message-error">{deployFailure}</span>
                    )}
                  </div>
                </li>
              );
            })}
          </ol>

          <aside className="getting-started-next-action">
            <span>{copy.nextAction}</span>
            <h3>{actionTitle}</h3>
            <p>{actionDescription}</p>
            {primaryServer && <small>{copy.serverName}: <strong>{primaryServer.name || primaryServer.id}</strong></small>}
            {actionInstall && installationBusy && <small className="getting-started-action-status">{copy.installCoreQueued}</small>}
            {actionInstall && installationFailure && !installationBusy && (
              <small className="getting-started-action-status getting-started-action-status-error">{installationFailure}</small>
            )}
            {actionDeploy && deploymentBusy && <small className="getting-started-action-status">{copy.deployQueued}</small>}
            {actionDeploy && deployFailure && !deploymentBusy && (
              <small className="getting-started-action-status getting-started-action-status-error">{deployFailure}</small>
            )}
            {actionInstall ? (
              <button className="getting-started-action" type="button" disabled={installationBusy} onClick={runInstallation}>
                {installationBusy ? copy.installCorePending : actionLabel}
              </button>
            ) : actionDeploy ? (
              <button className="getting-started-action" type="button" disabled={deploymentBusy} onClick={runDeployment}>
                {deploymentBusy ? copy.deployPending : actionLabel}
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
