import type { Locale } from '../../shared/i18n/i18n';

export interface VPNCoreMessages {
  title: string;
  subtitle: string;
  connectedFirst: string;
  connectedFirstDescription: string;
  unavailableStatus: string;
  legacyTitle: string;
  legacyStatus: string;
  legacyDescription: string;
  notInstalledTitle: string;
  notInstalledDescription: string;
  installedTitle: string;
  installedDescription: string;
  stoppedTitle: string;
  stoppedDescription: string;
  runningTitle: string;
  runningDescription: string;
  failedTitle: string;
  failedDescription: string;
  unknownTitle: string;
  unknownDescription: string;
  installAction: string;
  configureProtocol: string;
  startAction: string;
  stopAction: string;
  restartAction: string;
  retryAction: string;
  checkingAction: string;
  updateAction: string;
  operationPending: string;
  operationQueued: string;
  operationFailed: string;
  confirmStop: string;
  confirmInstall: string;
  installationPending: string;
  installationQueued: string;
  installationAwaitingHeartbeat: string;
  installationFailed: string;
  installationUnsupported: string;
  unsupportedPlatform: string;
  repositoryConfigurationFailed: string;
  packageInstallationFailed: string;
  installationVerificationFailed: string;
  unsupportedControls: string;
  version: string;
  service: string;
  serviceState: string;
  binaryPath: string;
  checkedAt: string;
  technicalDetails: string;
  notAvailable: string;
}

const messages: Record<Locale, VPNCoreMessages> = {
  en: {
    title: 'VPN service',
    subtitle: 'RouteGate checks VPN Core status and helps manage the VPN service on this server.',
    connectedFirst: 'Connect the server first',
    connectedFirstDescription: 'VPN service management becomes available after this server is connected to RouteGate.',
    unavailableStatus: 'Awaiting connection',
    legacyTitle: 'Update RouteGate Agent',
    legacyStatus: 'Update required',
    legacyDescription: 'This Agent does not report VPN Core status yet. Update it to enable guided VPN service management.',
    notInstalledTitle: 'VPN Core is not installed',
    notInstalledDescription: 'Install sing-box to prepare this server for VPN configuration deployment.',
    installedTitle: 'VPN Core is installed',
    installedDescription: 'sing-box is available. Configure the protocol and an active VPN account, then render, validate and apply a configuration. Agent starts the service during apply.',
    stoppedTitle: 'VPN service is stopped',
    stoppedDescription: 'For a new node, configure the protocol and an active VPN account, then render, validate and apply a configuration. Agent starts sing-box during apply. If a configuration is already applied, you can start the service again.',
    runningTitle: 'VPN service is running',
    runningDescription: 'sing-box is available and the system service is active.',
    failedTitle: 'VPN service needs attention',
    failedDescription: 'The sing-box service reported a failed state. Review technical details before retrying.',
    unknownTitle: 'VPN service state is unknown',
    unknownDescription: 'RouteGate could not determine the current sing-box service state.',
    installAction: 'Install sing-box',
    configureProtocol: 'Configure VPN protocol',
    startAction: 'Start service',
    stopAction: 'Stop service',
    restartAction: 'Restart service',
    retryAction: 'Check again',
    checkingAction: 'Checking...',
    updateAction: 'Update Agent',
    operationPending: 'Applying operation...',
    operationQueued: 'The operation was queued. RouteGate is waiting for the Agent to confirm the new service state.',
    operationFailed: 'The operation could not be queued.',
    confirmStop: 'Stop the VPN service? Connected VPN clients may be disconnected.',
    confirmInstall: 'Install sing-box on this server? RouteGate Agent will configure the supported repository and install the package without starting the VPN service.',
    installationPending: 'Installing sing-box...',
    installationQueued: 'Installation is queued and will start when RouteGate Agent claims the task.',
    installationAwaitingHeartbeat: 'Installation completed. Waiting for the next Agent heartbeat to confirm VPN Core state.',
    installationFailed: 'sing-box installation failed. Check the reported reason and node connectivity, then use Install sing-box to retry.',
    installationUnsupported: 'This Agent cannot install sing-box on the current platform.',
    unsupportedPlatform: 'Automatic installation supports Ubuntu LTS and Debian-compatible APT systems on amd64 or arm64.',
    repositoryConfigurationFailed: 'The supported sing-box repository could not be configured.',
    packageInstallationFailed: 'The sing-box package could not be installed.',
    installationVerificationFailed: 'sing-box was installed, but its binary or system service could not be verified.',
    unsupportedControls: 'Update RouteGate Agent to enable service controls.',
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
    subtitle: 'RouteGate проверяет состояние VPN Core и помогает управлять VPN-службой на этом сервере.',
    connectedFirst: 'Сначала подключите сервер',
    connectedFirstDescription: 'Управление VPN-службой станет доступно после подключения этого сервера к RouteGate.',
    unavailableStatus: 'Ожидает подключения',
    legacyTitle: 'Обновите RouteGate Agent',
    legacyStatus: 'Требуется обновление',
    legacyDescription: 'Эта версия Agent ещё не передаёт состояние VPN Core. Обновите её, чтобы включить управляемый сценарий VPN-службы.',
    notInstalledTitle: 'VPN Core не установлен',
    notInstalledDescription: 'Установите sing-box, чтобы подготовить сервер к развёртыванию VPN-конфигураций.',
    installedTitle: 'VPN Core установлен',
    installedDescription: 'sing-box доступен. Настройте протокол и активный VPN-аккаунт, затем выполните рендеринг, проверку и применение конфигурации. Agent запустит службу при применении.',
    stoppedTitle: 'VPN-служба остановлена',
    stoppedDescription: 'Для нового узла настройте протокол и активный VPN-аккаунт, затем выполните рендеринг, проверку и применение конфигурации. Agent запустит sing-box при применении. Если конфигурация уже применена, можно снова запустить службу.',
    runningTitle: 'VPN-служба работает',
    runningDescription: 'sing-box доступен, а системная служба активна.',
    failedTitle: 'VPN-служба требует внимания',
    failedDescription: 'Служба sing-box сообщила об ошибке. Перед повторной попыткой проверьте технические сведения.',
    unknownTitle: 'Состояние VPN-службы неизвестно',
    unknownDescription: 'RouteGate не смог определить текущее состояние службы sing-box.',
    installAction: 'Установить sing-box',
    configureProtocol: 'Настроить VPN-протокол',
    startAction: 'Запустить службу',
    stopAction: 'Остановить службу',
    restartAction: 'Перезапустить службу',
    retryAction: 'Проверить снова',
    checkingAction: 'Проверка...',
    updateAction: 'Обновить Agent',
    operationPending: 'Операция выполняется...',
    operationQueued: 'Операция поставлена в очередь. RouteGate ожидает подтверждения нового состояния от Agent.',
    operationFailed: 'Не удалось поставить операцию в очередь.',
    confirmStop: 'Остановить VPN-службу? Подключённые VPN-клиенты могут потерять соединение.',
    confirmInstall: 'Установить sing-box на этот сервер? RouteGate Agent настроит поддерживаемый репозиторий и установит пакет, не запуская VPN-службу.',
    installationPending: 'Установка sing-box...',
    installationQueued: 'Установка поставлена в очередь и начнётся, когда RouteGate Agent получит задачу.',
    installationAwaitingHeartbeat: 'Установка завершена. Ожидаем следующий heartbeat Agent для подтверждения состояния VPN Core.',
    installationFailed: 'Установка sing-box завершилась с ошибкой. Проверьте указанную причину и связь с узлом, затем повторите действие «Установить sing-box».',
    installationUnsupported: 'Этот Agent не может установить sing-box на текущей платформе.',
    unsupportedPlatform: 'Автоматическая установка поддерживает Ubuntu LTS и Debian-совместимые APT-системы на amd64 или arm64.',
    repositoryConfigurationFailed: 'Не удалось настроить поддерживаемый репозиторий sing-box.',
    packageInstallationFailed: 'Не удалось установить пакет sing-box.',
    installationVerificationFailed: 'sing-box установлен, но не удалось проверить бинарный файл или системную службу.',
    unsupportedControls: 'Обновите RouteGate Agent, чтобы включить управление службой.',
    version: 'Версия',
    service: 'Служба',
    serviceState: 'Состояние службы',
    binaryPath: 'Путь к бинарному файлу',
    checkedAt: 'Последняя проверка',
    technicalDetails: 'Технические сведения',
    notAvailable: 'Нет данных',
  },
};

export function getVPNCoreMessages(locale: Locale): VPNCoreMessages {
  return messages[locale];
}
