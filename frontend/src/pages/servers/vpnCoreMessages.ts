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
  retryAction: string;
  checkingAction: string;
  updateAction: string;
  plannedAction: string;
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
    subtitle: 'RouteGate checks the VPN Core installed on this server and shows the next recommended action.',
    connectedFirst: 'Connect the server first',
    connectedFirstDescription: 'VPN service management becomes available after this server is connected to RouteGate.',
    unavailableStatus: 'Unavailable',
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
    checkingAction: 'Checking...',
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
    connectedFirstDescription: 'Управление VPN-службой станет доступно после подключения этого сервера к RouteGate.',
    unavailableStatus: 'Недоступна',
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
    checkingAction: 'Проверка...',
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
};

export function getVPNCoreMessages(locale: Locale): VPNCoreMessages {
  return messages[locale];
}
