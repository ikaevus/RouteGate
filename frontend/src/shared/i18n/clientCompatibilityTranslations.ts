export const clientCompatibilityEn = {
  'clientCompatibility.title': 'Client compatibility',
  'clientCompatibility.smartRoutingTitle': 'Smart Routing compatibility',
  'clientCompatibility.full': 'Full smart routing support',
  'clientCompatibility.setup': 'Supported with client-side setup',
  'clientCompatibility.partial': 'Partial compatibility',
  'clientCompatibility.connectionOnly': 'Connection only',
  'clientCompatibility.preferredDelivery': 'Subscription format',
  'clientCompatibility.noSilentDowngrade': 'A Routing Profile is assigned, but this client cannot be treated as fully RouteGate-managed. Complete the client guidance before considering DIRECT/VPN/BLOCK policy enforced.',
  'clientCompatibility.unavailable': 'Could not determine the selected VPN client capabilities. Do not treat Smart Routing as guaranteed.',
  'clientCompatibility.secureSubscription': 'Secure subscription URL',
  'clientCompatibility.subscriptionDescription': 'The RG-115 URL stays short and opaque. RG-115A selects the configuration representation for the chosen VPN client; raw protocol material remains a manual fallback.',
  'clientCompatibility.hiddify': 'Hiddify',
} as const;

export const clientCompatibilityRu: Record<keyof typeof clientCompatibilityEn, string> = {
  'clientCompatibility.title': 'Совместимость клиента',
  'clientCompatibility.smartRoutingTitle': 'Совместимость Smart Routing',
  'clientCompatibility.full': 'Полная поддержка Smart Routing',
  'clientCompatibility.setup': 'Поддерживается с настройкой клиента',
  'clientCompatibility.partial': 'Частичная совместимость',
  'clientCompatibility.connectionOnly': 'Только подключение',
  'clientCompatibility.preferredDelivery': 'Формат подписки',
  'clientCompatibility.noSilentDowngrade': 'Routing Profile назначен, но этот клиент не может считаться полностью управляемым RouteGate. Выполните указанные настройки клиента перед тем, как считать DIRECT/VPN/BLOCK политику применённой.',
  'clientCompatibility.unavailable': 'Не удалось определить возможности выбранного VPN-клиента. Не считайте Smart Routing гарантированным.',
  'clientCompatibility.secureSubscription': 'Безопасный URL подписки',
  'clientCompatibility.subscriptionDescription': 'RG-115 URL остаётся коротким и непрозрачным. RG-115A выбирает представление конфигурации по выбранному VPN-клиенту; raw URI остаётся ручным fallback.',
  'clientCompatibility.hiddify': 'Hiddify',
};

export type ClientCompatibilityTranslationKey = keyof typeof clientCompatibilityEn;
