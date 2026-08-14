export const deploymentHistoryEn = {
  'serverDetails.deploymentHistoryImmutableHint': 'Deployment records are operational history, not editable configuration. RouteGate keeps up to 100 completed jobs per server automatically; completed records may also be cleared manually without affecting audit history.',
  'serverDetails.clearDeploymentHistory': 'Clear completed history',
  'serverDetails.clearDeploymentHistoryConfirm': 'Delete all completed deployment records for this server? Pending and in-progress deployments will be kept. Audit history is not affected.',
  'serverDetails.clearDeploymentHistorySuccess': 'Completed deployment history cleared.',
  'serverDetails.clearDeploymentHistoryError': 'Could not clear completed deployment history.',
  'serverDetails.deploymentHistoryRange': 'Showing {from}–{to} of {total}',
  'serverDetails.deploymentHistoryPage': 'Page {page} of {pages}',
  'serverDetails.deploymentHistoryPrevious': 'Previous',
  'serverDetails.deploymentHistoryNext': 'Next',
} as const;

export type DeploymentHistoryTranslationKey = keyof typeof deploymentHistoryEn;

export const deploymentHistoryRu: Record<DeploymentHistoryTranslationKey, string> = {
  'serverDetails.deploymentHistoryImmutableHint': 'История развёртываний — это эксплуатационные записи, а не редактируемая конфигурация. RouteGate автоматически хранит до 100 завершённых задач на сервер; завершённые записи также можно очистить вручную без изменения журнала аудита.',
  'serverDetails.clearDeploymentHistory': 'Очистить завершённую историю',
  'serverDetails.clearDeploymentHistoryConfirm': 'Удалить все завершённые записи о развёртываниях этого сервера? Ожидающие и выполняющиеся развёртывания будут сохранены. Журнал аудита не изменится.',
  'serverDetails.clearDeploymentHistorySuccess': 'Завершённая история развёртываний очищена.',
  'serverDetails.clearDeploymentHistoryError': 'Не удалось очистить завершённую историю развёртываний.',
  'serverDetails.deploymentHistoryRange': 'Показано {from}–{to} из {total}',
  'serverDetails.deploymentHistoryPage': 'Страница {page} из {pages}',
  'serverDetails.deploymentHistoryPrevious': 'Назад',
  'serverDetails.deploymentHistoryNext': 'Далее',
};
