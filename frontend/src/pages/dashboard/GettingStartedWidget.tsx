import { useEffect, useState } from 'react';
import { useMutation, useQueries, useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getManagerHealth } from '../../entities/health/api/healthApi';
import {
  applyConfigVersion,
  getConfigApplyJobs,
  getProtocolSettings,
  getServers,
  renderConfig,
  validateConfigVersion,
  type Server,
} from '../../entities/server/api/serverApi';
import {
  createVPNCoreInstallation,
  getVPNCoreInstallation,
} from '../../entities/server/api/vpnCoreApi';
import { parseVPNCoreStatus } from '../../entities/server/model/vpnCoreStatus';
import { getVpnAccessSummary } from '../../entities/vpnAccount/api/vpnAccountApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';
import {
  nodeCompletedSteps,
  nodeSetupStage,
  protocolConfigured,
  selectGettingStartedNode,
  setupStepKeys,
  type NodeSetupFacts,
  type NodeSetupStage,
  type SetupStepKey,
} from './gettingStartedModel';
import './getting-started.css';

type SetupStepState = 'complete' | 'current' | 'pending';

type SetupStepText = {
  label: string;
  description: string;
};

type SetupStep = {
  key: SetupStepKey;
  complete: boolean;
  copy: Record<SetupStepState, SetupStepText>;
  to?: string | null;
};

const dismissedStorageKey = 'routegate.gettingStarted.dismissed';

function supportsInstallation(capabilities?: Record<string, unknown>): boolean {
  const value = capabilities?.vpnCoreInstallationOperations;
  return Array.isArray(value) && value.includes('install_sing_box');
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() ? error.message : fallback;
}

function nodeLabel(node: { name?: string | null; id: string }): string {
  return node.name?.trim() || node.id;
}

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      eyebrow: 'Первоначальная настройка',
      title: 'Начало работы',
      subtitle: 'RouteGate ведёт вас к первому рабочему VPN на одном узле. Выполняйте шаги по порядку — следующий нужный переход всегда показан справа.',
      nodeContext: 'Узел',
      nodeContextHint: 'Шаги 2–5 относятся только к этому узлу.',
      checking: 'Проверяем состояние RouteGate…',
      checkFailed: 'Не удалось определить состояние первоначальной настройки.',
      accessUnknown: (servers: string[]) => `Не удалось проверить, выдаёт ли доступ ${servers.length === 1 ? 'узел' : 'узлы'} ${servers.join(', ')}. Это не означает, что VPN не работает; повторите проверку.`,
      retry: 'Проверить снова',
      progress: (done: number, total: number) => `${done} из ${total}`,
      complete: 'Готово',
      pending: 'Ожидает',
      current: 'Сейчас',
      nextAction: 'Следующий шаг',
      serverName: 'Узел',
      openStep: 'Открыть',
      installed: {
        complete: { label: 'RouteGate установлен', description: 'Manager и веб-интерфейс доступны.' },
        current: { label: 'Проверить RouteGate', description: 'Проверяем доступность Manager и веб-интерфейса.' },
        pending: { label: 'Проверить RouteGate', description: 'Сначала RouteGate должен подтвердить готовность Manager.' },
      },
      server: {
        complete: { label: 'Узел подключён', description: 'Agent этого узла онлайн.' },
        current: { label: 'Подключить узел', description: 'Подключите узел и дождитесь, когда его Agent станет онлайн.' },
        pending: { label: 'Подключить узел', description: 'Этот шаг станет доступен после проверки RouteGate.' },
      },
      core: {
        complete: { label: 'VPN Core установлен', description: 'VPN Core этого узла установлен и готов к конфигурации.' },
        current: { label: 'Установить VPN Core', description: 'Подготовьте на этом узле runtime для выбранного VPN-протокола.' },
        pending: { label: 'Установить VPN Core', description: 'Этот шаг станет доступен после подключения узла.' },
      },
      protocol: {
        complete: { label: 'VPN-протокол настроен', description: 'Параметры и ключи протокола этого узла сохранены.' },
        current: { label: 'Настроить VPN-протокол', description: 'Выберите поддерживаемый VPN-протокол и сохраните рекомендуемые параметры для этого узла.' },
        pending: { label: 'Настроить VPN-протокол', description: 'Этот шаг станет доступен после установки VPN Core.' },
      },
      final: {
        complete: { label: 'VPN-доступ выдаётся', description: 'Применённая конфигурация узла обслуживает активный аккаунт.' },
        current: { label: 'Создать VPN-аккаунт', description: 'Создайте первый активный аккаунт и привяжите его к этому узлу.' },
        pending: { label: 'Создать VPN-аккаунт', description: 'Этот шаг станет доступен после настройки VPN-протокола.' },
      },
      deployCurrent: {
        label: 'Применить VPN-конфигурацию',
        description: 'Аккаунт есть, но применённая конфигурация узла его ещё не обслуживает. RouteGate отрендерит, проверит и применит конфигурацию через Agent.',
      },
      accessCurrent: {
        label: 'Проверить доступ аккаунта',
        description: 'Конфигурация узла применена, но аккаунт пока не получает доступ. Откройте аккаунт, чтобы увидеть причину.',
      },
      systemActionTitle: 'Проверить RouteGate',
      systemActionDescription: 'Manager пока не подтвердил готовность. Повторим проверку состояния.',
      addServerTitle: 'Подключить узел',
      addServerDescription: 'Agent этого узла не в сети. Откройте узел и завершите подключение.',
      addFirstServerDescription: 'RouteGate не видит ни одного VPN-узла. Добавьте узел и подключите его Agent.',
      addServerAction: 'Открыть узел',
      addFirstServerAction: 'Открыть серверы',
      installCoreTitle: 'Установить VPN Core',
      installCoreDescription: 'RouteGate установит sing-box на этот узел через его Agent и проверит результат.',
      installCoreAction: 'Установить',
      installCorePending: 'Устанавливаем…',
      installCoreQueued: 'Установка выполняется через RouteGate Agent. Этот шаг обновится автоматически.',
      installCoreFailed: 'Не удалось установить VPN Core. Можно повторить попытку или открыть узел для подробностей.',
      installCoreConfirm: (server: string) => `Установить VPN Core на ${server}?\n\nRouteGate установит sing-box. Сервис будет запущен позже, после создания и применения рабочего VPN-конфига.`,
      openCoreAction: 'Открыть VPN Core',
      protocolTitle: 'Настроить VPN-протокол',
      protocolDescription: 'Выберите поддерживаемый VPN-протокол и настройте его для этого узла.',
      protocolAction: 'Настроить протокол',
      accountTitle: 'Создать первый VPN-аккаунт',
      accountDescription: 'Создайте активный аккаунт и привяжите его к этому узлу.',
      accountAction: 'Создать VPN-аккаунт',
      deployTitle: 'Применить VPN-конфигурацию',
      deployDescription: 'Аккаунт этого узла ещё не входит в применённую конфигурацию. RouteGate отрендерит, проверит и применит её; Agent выполнит необходимые перезапуски и healthcheck.',
      deployAction: 'Применить конфигурацию',
      deployPending: 'Применяем…',
      deployQueued: 'Конфигурация применяется через RouteGate Agent. Состояние обновится автоматически.',
      deployFailed: 'Не удалось применить VPN-конфигурацию. Можно повторить попытку или открыть узел для подробностей.',
      deployLastFailed: (message: string) => `Последнее применение на этом узле завершилось ошибкой: ${message}`,
      deployValidationFailed: 'Сгенерированная VPN-конфигурация не прошла проверку.',
      deployConfirm: (server: string) => `Применить VPN-конфигурацию на ${server}?\n\nRouteGate отрендерит, проверит и применит конфиг. После применения Agent выполнит необходимые перезапуски и healthcheck.`,
      accessTitle: 'Проверить доступ аккаунта',
      accessDescription: 'Конфигурация узла применена, но аккаунт пока не получает доступ.',
      accessAction: 'Открыть аккаунт',
      readyTitle: 'RouteGate готов',
      readyDescription: 'Конфигурация узла применена через Agent, и активный аккаунт получает доступ по ней. Подключение клиента проверьте на устройстве.',
      readyAction: 'Открыть доступ устройства',
      dismiss: 'Скрыть',
      readyServer: (servers: string[]) => (servers.length === 1 ? `Рабочий узел: ${servers[0]}` : `Рабочие узлы: ${servers.join(', ')}`),
      otherNodes: (servers: string[]) => `Пока без выданного доступа: ${servers.join(', ')}. Первоначальная настройка от этих узлов не зависит.`,
      otherNodesUnchecked: (servers: string[]) => `Проверка доступа не завершена: ${servers.join(', ')}. RouteGate повторит её автоматически.`,
    } as const;
  }

  return {
    eyebrow: 'First-run setup',
    title: 'Getting started',
    subtitle: 'RouteGate guides you to your first working VPN on one node. Complete the steps in order — the next required action is always shown on the right.',
    nodeContext: 'Node',
    nodeContextHint: 'Steps 2–5 apply to this node only.',
    checking: 'Checking RouteGate setup state…',
    checkFailed: 'RouteGate could not determine the first-run setup state.',
    accessUnknown: (servers: string[]) => `RouteGate could not check whether ${servers.length === 1 ? 'node' : 'nodes'} ${servers.join(', ')} ${servers.length === 1 ? 'issues' : 'issue'} access. This does not mean the VPN is down; check again.`,
    retry: 'Check again',
    progress: (done: number, total: number) => `${done} of ${total}`,
    complete: 'Complete',
    pending: 'Pending',
    current: 'Now',
    nextAction: 'Next action',
    serverName: 'Node',
    openStep: 'Open',
    installed: {
      complete: { label: 'RouteGate installed', description: 'Manager and the web interface are available.' },
      current: { label: 'Check RouteGate', description: 'Checking that Manager and the web interface are available.' },
      pending: { label: 'Check RouteGate', description: 'RouteGate must confirm Manager readiness first.' },
    },
    server: {
      complete: { label: 'Node connected', description: 'This node\'s Agent is online.' },
      current: { label: 'Connect the node', description: 'Connect the node and wait for its Agent to come online.' },
      pending: { label: 'Connect the node', description: 'This step becomes available after RouteGate is ready.' },
    },
    core: {
      complete: { label: 'VPN Core installed', description: 'This node\'s VPN Core is installed and ready for configuration.' },
      current: { label: 'Install VPN Core', description: 'Prepare the runtime for the selected VPN protocol on this node.' },
      pending: { label: 'Install VPN Core', description: 'This step becomes available after the node is connected.' },
    },
    protocol: {
      complete: { label: 'VPN protocol configured', description: 'This node\'s protocol settings and keys are saved.' },
      current: { label: 'Configure VPN protocol', description: 'Choose a supported VPN protocol and save the recommended settings for this node.' },
      pending: { label: 'Configure VPN protocol', description: 'This step becomes available after VPN Core is installed.' },
    },
    final: {
      complete: { label: 'VPN access issued', description: 'The node\'s applied configuration serves an active account.' },
      current: { label: 'Create VPN account', description: 'Create the first active account and assign it to this node.' },
      pending: { label: 'Create VPN account', description: 'This step becomes available after the VPN protocol is configured.' },
    },
    deployCurrent: {
      label: 'Apply VPN configuration',
      description: 'An account exists, but the node\'s applied configuration does not serve it yet. RouteGate will render, validate, and apply the configuration through Agent.',
    },
    accessCurrent: {
      label: 'Check account access',
      description: 'The node\'s configuration is applied, but the account does not get access yet. Open the account to see why.',
    },
    systemActionTitle: 'Check RouteGate',
    systemActionDescription: 'Manager has not confirmed readiness yet. Check the setup state again.',
    addServerTitle: 'Connect the node',
    addServerDescription: 'This node\'s Agent is not online. Open the node and finish the connection.',
    addFirstServerDescription: 'RouteGate does not see any VPN node. Add a node and connect its Agent.',
    addServerAction: 'Open node',
    addFirstServerAction: 'Open Servers',
    installCoreTitle: 'Install VPN Core',
    installCoreDescription: 'RouteGate will install sing-box on this node through its Agent and verify the result.',
    installCoreAction: 'Install',
    installCorePending: 'Installing…',
    installCoreQueued: 'Installation is running through RouteGate Agent. This step will update automatically.',
    installCoreFailed: 'VPN Core installation failed. You can retry or open the node for details.',
    installCoreConfirm: (server: string) => `Install VPN Core on ${server}?\n\nRouteGate will install sing-box. The service will be started later, after a working VPN configuration is created and applied.`,
    openCoreAction: 'Open VPN Core',
    protocolTitle: 'Configure VPN protocol',
    protocolDescription: 'Choose a supported VPN protocol and configure it for this node.',
    protocolAction: 'Configure protocol',
    accountTitle: 'Create your first VPN account',
    accountDescription: 'Create an active account and assign it to this node.',
    accountAction: 'Create VPN account',
    deployTitle: 'Apply VPN configuration',
    deployDescription: 'This node\'s account is not in the applied configuration yet. RouteGate will render, validate, and apply it; Agent will perform the required restarts and healthcheck.',
    deployAction: 'Apply configuration',
    deployPending: 'Applying…',
    deployQueued: 'Configuration is being applied through RouteGate Agent. This state will update automatically.',
    deployFailed: 'Applying the VPN configuration failed. You can retry or open the node for details.',
    deployLastFailed: (message: string) => `The last apply on this node failed: ${message}`,
    deployValidationFailed: 'The generated VPN configuration did not pass validation.',
    deployConfirm: (server: string) => `Apply the VPN configuration to ${server}?\n\nRouteGate will render, validate, and apply the config. After applying it, Agent will perform the required restarts and healthcheck.`,
    accessTitle: 'Check account access',
    accessDescription: 'The node\'s configuration is applied, but the account does not get access yet.',
    accessAction: 'Open account',
    readyTitle: 'RouteGate is ready',
    readyDescription: 'The node\'s configuration is applied through Agent and an active account gets access from it. Check the client connection on the device.',
    readyAction: 'Open device access',
    dismiss: 'Hide',
    readyServer: (servers: string[]) => (servers.length === 1 ? `Working node: ${servers[0]}` : `Working nodes: ${servers.join(', ')}`),
    otherNodes: (servers: string[]) => `No access issued yet: ${servers.join(', ')}. First-run setup does not depend on these nodes.`,
    otherNodesUnchecked: (servers: string[]) => `Access check not finished: ${servers.join(', ')}. RouteGate will retry it automatically.`,
  } as const;
}

export function GettingStartedWidget() {
  const copy = getCopy();
  const [installation, setInstallation] = useState<{ serverId: string; jobId: string } | null>(null);
  const [installationFailure, setInstallationFailure] = useState<string | null>(null);
  const [deployment, setDeployment] = useState<{ serverId: string; jobId: string } | null>(null);
  const [deployFailure, setDeployFailure] = useState<string | null>(null);
  const [dismissed, setDismissed] = useState(() => {
    try {
      return window.localStorage.getItem(dismissedStorageKey) === 'true';
    } catch {
      return false;
    }
  });
  const fastPolling = Boolean(installation || deployment);

  const managerHealthQuery = useQuery({
    queryKey: ['manager-health'],
    queryFn: getManagerHealth,
    refetchInterval: 10_000,
  });

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
    refetchInterval: fastPolling ? 2_000 : 10_000,
  });

  // Per node: does it issue client access to at least one active account?
  // Evaluated by the Manager with the rules that issue client links, over all
  // active accounts, without writing anything.
  const accessQuery = useQuery({
    queryKey: ['vpn-access-summary'],
    queryFn: getVpnAccessSummary,
    refetchInterval: fastPolling ? 2_000 : 15_000,
  });

  // Management-only nodes never serve VPN accounts; every other node is
  // evaluated on its own.
  const vpnNodes: Server[] = (serversQuery.data?.items ?? []).filter((server) => server.deploymentRole !== 'management');
  const accessByNode = new Map((accessQuery.data?.items ?? []).map((item) => [item.serverId, item]));
  const onlineNodes = vpnNodes.filter((server) => server.agent?.status === 'online');
  const nodesWithAccounts = onlineNodes.filter((server) => (accessByNode.get(server.id)?.activeAccounts ?? 0) > 0);

  const protocolQueries = useQueries({
    queries: onlineNodes.map((server) => ({
      queryKey: ['server-protocol-settings', server.id],
      queryFn: () => getProtocolSettings(server.id),
      retry: false,
      refetchInterval: 10_000,
    })),
  });
  const applyJobQueries = useQueries({
    queries: nodesWithAccounts.map((server) => ({
      queryKey: ['server-config-apply-jobs', server.id],
      queryFn: () => getConfigApplyJobs(server.id),
      refetchInterval: deployment?.serverId === server.id ? 2_000 : 10_000,
    })),
  });

  const protocolByNode = new Map(onlineNodes.map((server, index) => [server.id, protocolQueries[index]]));
  const applyJobsByNode = new Map(nodesWithAccounts.map((server, index) => [server.id, applyJobQueries[index]]));

  const facts: NodeSetupFacts[] = vpnNodes.map((server) => {
    const online = server.agent?.status === 'online';
    const protocol = protocolByNode.get(server.id)?.data;
    const access = accessByNode.get(server.id);
    const latestJob = applyJobsByNode.get(server.id)?.data?.items?.[0];
    return {
      id: server.id,
      name: nodeLabel(server),
      agentOnline: online,
      coreInstalled: online && Boolean(parseVPNCoreStatus(server.agent?.capabilities, protocol?.protocol)?.installed),
      protocolConfigured: online && protocolConfigured(protocol),
      activeAccountCount: access?.activeAccounts ?? 0,
      access: access?.state ?? 'not_served',
      servedAccountId: access?.servedAccountId ?? null,
      pendingAccountId: access?.pendingAccountId ?? null,
      pendingStatus: access?.pendingStatus ?? null,
      pendingMessage: access?.pendingMessage ?? null,
      latestApplyFailed: latestJob?.status === 'failed',
      latestApplyError: latestJob?.errorMessage ?? null,
    };
  });

  const selection = selectGettingStartedNode(facts);
  const focus = selection.focus;
  const focusServer = focus ? vpnNodes.find((server) => server.id === focus.id) ?? null : null;
  const focusStage: NodeSetupStage | null = focus ? nodeSetupStage(focus) : null;
  const managerReady = managerHealthQuery.isSuccess;
  const installationSupported = supportsInstallation(focusServer?.agent?.capabilities);
  const focusProtocolQuery = focus ? protocolByNode.get(focus.id) : undefined;
  const focusApplyJobsQuery = focus ? applyJobsByNode.get(focus.id) : undefined;

  const installationMutation = useMutation({
    mutationFn: (serverId: string) => createVPNCoreInstallation(serverId),
    onSuccess: ({ job }, serverId) => {
      setInstallationFailure(null);
      setInstallation({ serverId, jobId: job.id });
      void serversQuery.refetch();
    },
    onError: (error) => {
      setInstallationFailure(errorMessage(error, copy.installCoreFailed));
    },
  });

  const installationQuery = useQuery({
    queryKey: ['vpn-core-installation', installation?.serverId, installation?.jobId],
    queryFn: () => getVPNCoreInstallation(installation?.serverId ?? '', installation?.jobId ?? ''),
    enabled: Boolean(installation),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'failed' || status === 'succeeded' ? false : 2_000;
    },
  });

  const deploymentMutation = useMutation({
    mutationFn: async (serverId: string) => {
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
    onSuccess: ({ job }, serverId) => {
      setDeployFailure(null);
      setDeployment({ serverId, jobId: job.id });
      void accessQuery.refetch();
      void focusApplyJobsQuery?.refetch();
      void serversQuery.refetch();
    },
    onError: (error) => {
      setDeployFailure(errorMessage(error, copy.deployFailed));
    },
  });

  const installedOnInstallationNode = installation
    ? facts.find((node) => node.id === installation.serverId)?.coreInstalled ?? false
    : false;

  useEffect(() => {
    const job = installationQuery.data;
    if (!installation || !job) return;

    if (job.status === 'failed') {
      setInstallationFailure(
        typeof job.errorMessage === 'string' && job.errorMessage.trim()
          ? job.errorMessage.trim()
          : copy.installCoreFailed,
      );
      setInstallation(null);
      return;
    }

    if (job.status === 'succeeded') {
      void serversQuery.refetch();
      if (installedOnInstallationNode) {
        setInstallation(null);
      }
    }
  }, [installation, installationQuery.data, installedOnInstallationNode]);

  const deploymentJobs = deployment ? applyJobsByNode.get(deployment.serverId)?.data?.items : undefined;
  const activeDeployJob = deployment
    ? deploymentJobs?.find((job) => job.id === deployment.jobId) ?? null
    : null;

  useEffect(() => {
    if (!deployment || !activeDeployJob) return;

    if (activeDeployJob.status === 'failed') {
      setDeployFailure(activeDeployJob.errorMessage?.trim() || copy.deployFailed);
      setDeployment(null);
      return;
    }

    if (activeDeployJob.status === 'succeeded') {
      // A terminal Agent result must always release the action immediately.
      // Subsequent refetches determine whether the step is complete; they must
      // not keep the UI stuck in a synthetic "Applying" state.
      setDeployment(null);
      void applyJobsByNode.get(deployment.serverId)?.refetch();
      void accessQuery.refetch();
      void serversQuery.refetch();
    }
  }, [activeDeployJob, deployment]);

  const installationBusy = installationMutation.isPending || installation !== null;
  const deploymentBusy = deploymentMutation.isPending || deployment !== null;
  const inlineInstallAvailable = Boolean(managerReady && focus && focusStage === 'core' && installationSupported);
  const deployAvailable = Boolean(managerReady && focus && focusStage === 'apply');

  const runInstallation = () => {
    if (!focus || installationBusy || !inlineInstallAvailable) return;
    if (!window.confirm(copy.installCoreConfirm(focus.name))) return;

    installationMutation.reset();
    setInstallationFailure(null);
    installationMutation.mutate(focus.id);
  };

  const runDeployment = () => {
    if (!focus || !deployAvailable || deploymentBusy) return;
    if (!window.confirm(copy.deployConfirm(focus.name))) return;

    deploymentMutation.reset();
    setDeployFailure(null);
    deploymentMutation.mutate(focus.id);
  };

  const completedCount = !managerReady ? 0 : focus ? nodeCompletedSteps(focus) : 1;
  const finalCopy = focusStage === 'apply'
    ? { ...copy.final, current: copy.deployCurrent }
    : focusStage === 'access'
      ? { ...copy.final, current: copy.accessCurrent }
      : copy.final;
  const nodePath = focus ? `/servers/${encodeURIComponent(focus.id)}` : '/servers';
  const finalTo = focusStage === 'ready' && focus?.servedAccountId
    ? `/vpn-accounts/${encodeURIComponent(focus.servedAccountId)}/access`
    : focusStage === 'access' && focus?.pendingAccountId
      ? `/vpn-accounts/${encodeURIComponent(focus.pendingAccountId)}/access`
      : focusStage === 'apply'
        ? nodePath
        : focus
          ? `/vpn-accounts?create=1&server=${encodeURIComponent(focus.id)}`
          : '/vpn-accounts?create=1';

  const stepTargets: Record<SetupStepKey, string | null> = {
    installed: null,
    server: nodePath,
    core: focus ? nodePath : null,
    protocol: focus ? `/protocol-settings/${encodeURIComponent(focus.id)}` : null,
    final: finalTo,
  };
  const stepCopy: Record<SetupStepKey, Record<SetupStepState, SetupStepText>> = {
    installed: copy.installed,
    server: copy.server,
    core: copy.core,
    protocol: copy.protocol,
    final: finalCopy,
  };
  const steps: SetupStep[] = setupStepKeys.map((key, index) => ({
    key,
    copy: stepCopy[key],
    complete: index < completedCount,
    to: stepTargets[key],
  }));

  const currentStepIndex = steps.findIndex((step) => !step.complete);
  const allReady = managerReady && selection.setupComplete;
  const pendingQuery = (query?: { isPending: boolean }) => Boolean(query?.isPending);
  const loading = managerHealthQuery.isPending
    || serversQuery.isPending
    || accessQuery.isPending
    || protocolQueries.some(pendingQuery)
    || applyJobQueries.some(pendingQuery);
  const failedQuery = (query?: { isError: boolean }) => Boolean(query?.isError);
  // A failed load, or a node whose access could not be evaluated while no node
  // is known to work, leaves the state undetermined: never "no working VPN".
  const undetermined = !selection.setupComplete && selection.undeterminedNodes.length > 0;
  const failed = managerHealthQuery.isError
    || serversQuery.isError
    || accessQuery.isError
    || protocolQueries.some(failedQuery)
    || applyJobQueries.some(failedQuery)
    || undetermined;

  useEffect(() => {
    if (!loading && !failed && !allReady && dismissed) {
      setDismissed(false);
      try {
        window.localStorage.removeItem(dismissedStorageKey);
      } catch {
        // Local persistence is optional; state-aware restoration still wins in-memory.
      }
    }
  }, [loading, failed, allReady, dismissed]);

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
    void accessQuery.refetch();
    for (const query of [...protocolQueries, ...applyJobQueries]) {
      void query.refetch();
    }
  };

  // A hidden guide stays hidden while access is merely undetermined.
  if (!loading && dismissed && ((!failed && allReady) || undetermined)) {
    return null;
  }

  if (!loading && !failed && allReady && focus?.servedAccountId) {
    const others = selection.otherNodes.map((node) => node.name);
    const unchecked = selection.undeterminedNodes.map((node) => node.name);
    return (
      <section className="dashboard-widget getting-started-widget getting-started-widget-complete" aria-labelledby="getting-started-complete-title">
        <div className="getting-started-complete-layout">
          <span className="getting-started-complete-check" aria-hidden="true">✓</span>
          <div className="getting-started-complete-copy">
            <span className="getting-started-eyebrow">{copy.eyebrow}</span>
            <h2 id="getting-started-complete-title">{copy.readyTitle}</h2>
            <p>{copy.readyDescription}</p>
            <small>{copy.readyServer(selection.workingNodes.map((node) => node.name))}</small>
            {others.length > 0 && <small>{copy.otherNodes(others)}</small>}
            {unchecked.length > 0 && <small>{copy.otherNodesUnchecked(unchecked)}</small>}
          </div>
          <div className="getting-started-complete-actions">
            <Link className="getting-started-action" to={`/vpn-accounts/${encodeURIComponent(focus.servedAccountId)}/access`}>
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
  let actionNote: string | null = null;

  if (managerReady && !focus) {
    actionTitle = copy.addServerTitle;
    actionDescription = copy.addFirstServerDescription;
    actionLabel = copy.addFirstServerAction;
    actionTo = '/servers';
  } else if (managerReady && focus) {
    switch (focusStage) {
      case 'connect':
        actionTitle = copy.addServerTitle;
        actionDescription = copy.addServerDescription;
        actionLabel = copy.addServerAction;
        actionTo = nodePath;
        break;
      case 'core':
        actionTitle = copy.installCoreTitle;
        actionDescription = copy.installCoreDescription;
        if (installationSupported) {
          actionLabel = installationBusy ? copy.installCorePending : copy.installCoreAction;
          actionInstall = true;
        } else {
          actionLabel = copy.openCoreAction;
          actionTo = nodePath;
        }
        break;
      case 'protocol':
        actionTitle = copy.protocolTitle;
        actionDescription = copy.protocolDescription;
        actionLabel = copy.protocolAction;
        actionTo = `/protocol-settings/${encodeURIComponent(focus.id)}`;
        break;
      case 'account':
        actionTitle = copy.accountTitle;
        actionDescription = copy.accountDescription;
        actionLabel = copy.accountAction;
        actionTo = finalTo;
        break;
      case 'apply':
        actionTitle = copy.deployTitle;
        actionDescription = copy.deployDescription;
        actionLabel = deploymentBusy ? copy.deployPending : copy.deployAction;
        actionDeploy = true;
        if (focus.latestApplyFailed && !deployFailure) {
          actionNote = copy.deployLastFailed(focus.latestApplyError?.trim() || copy.deployFailed);
        }
        break;
      case 'access':
        actionTitle = copy.accessTitle;
        actionDescription = focus.pendingMessage?.trim() || copy.accessDescription;
        actionLabel = copy.accessAction;
        actionTo = finalTo;
        break;
      default:
        break;
    }
  }

  return (
    <section className="dashboard-widget getting-started-widget" aria-labelledby="getting-started-title">
      <div className="getting-started-header">
        <div>
          <span className="getting-started-eyebrow">{copy.eyebrow}</span>
          <h2 id="getting-started-title">{copy.title}</h2>
          <p>{copy.subtitle}</p>
          {focus && !loading && !failed && (
            <p className="getting-started-node" data-node-id={focus.id}>
              <span>{copy.nodeContext}:</span> <Link to={nodePath}>{focus.name}</Link>
              <small>{copy.nodeContextHint}</small>
            </p>
          )}
        </div>
        {!failed && (
          <div className="getting-started-progress-summary">
            <strong>{copy.progress(completedCount, steps.length)}</strong>
            <span>{copy.current}</span>
          </div>
        )}
      </div>

      {!failed && (
        <div className="getting-started-progress" aria-hidden="true">
          <span style={{ width: `${(completedCount / steps.length) * 100}%` }} />
        </div>
      )}

      {loading ? (
        <div className="getting-started-status">{copy.checking}</div>
      ) : failed ? (
        <div className="getting-started-status getting-started-status-error">
          <span>{undetermined && !loading ? copy.accessUnknown(selection.undeterminedNodes.map((node) => node.name)) : copy.checkFailed}</span>
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
              const showInlineDeploy = step.key === 'final' && isCurrent && deployAvailable;
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
            {focus && <small>{copy.serverName}: <strong>{focus.name}</strong></small>}
            {actionNote && !deploymentBusy && (
              <small className="getting-started-action-status getting-started-action-status-error">{actionNote}</small>
            )}
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
