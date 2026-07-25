import type { Locale } from '../../shared/i18n/i18n';

export interface VPNCoreMessages {
  title: string;
  subtitle: string;
  connectedFirst: string;
  connectedFirstDescription: string;
  unavailableStatus: string;
  legacyTitle: string;
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
    subtitle: 'RouteGate проверяет состояние VPN Core и помогает управлять VPN-службой этого сервера.',
    connectedFirst: 'Сначала подключите сервер',
    connectedFirstDescription: 'Управление VPN-службой станет доступно после подключения этого сервера к RouteGate.',
    unavailableStatus: 'Ожидает подключения',
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
