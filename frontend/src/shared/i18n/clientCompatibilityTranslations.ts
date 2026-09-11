export const clientCompatibilityEn = {
  'clientCompatibility.title': 'Client compatibility',
  'clientCompatibility.full': 'Full RouteGate',
  'clientCompatibility.setup': 'Compatible',
  'clientCompatibility.connectionOnly': 'Generic',
  'clientCompatibility.hiddify': 'Hiddify',
  'clientCompatibility.copySubscriptionUrl': 'Copy link',
  'clientCompatibility.close': 'Close',
  'clientCompatibility.copied': 'Copied',
} as const;

export const clientCompatibilityRu: Record<keyof typeof clientCompatibilityEn, string> = {
  'clientCompatibility.title': 'Совместимость клиента',
  'clientCompatibility.full': 'Полностью RouteGate',
  'clientCompatibility.setup': 'Совместим',
  'clientCompatibility.connectionOnly': 'Универсальный',
  'clientCompatibility.hiddify': 'Hiddify',
  'clientCompatibility.copySubscriptionUrl': 'Копировать ссылку',
  'clientCompatibility.close': 'Закрыть',
  'clientCompatibility.copied': 'Скопировано',
};

export type ClientCompatibilityTranslationKey = keyof typeof clientCompatibilityEn;
