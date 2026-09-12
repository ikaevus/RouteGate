export const clientCompatibilityEn = {
  'clientCompatibility.title': 'Client compatibility',
  'clientCompatibility.full': 'Full RouteGate',
  'clientCompatibility.setup': 'Compatible',
  'clientCompatibility.connectionOnly': 'Generic',
  'clientCompatibility.hiddify': 'Hiddify',
  'clientCompatibility.copySubscriptionUrl': 'Copy link',
  'clientCompatibility.close': 'Close',
  'clientCompatibility.copied': 'Copied',
  // Guidance/limitation copy, keyed by the stable codes the backend returns
  // in ClientCompatibilityAssessment.guidanceCodes/limitationCodes. The
  // backend never sends localized prose; this is the only place these
  // messages are written out, in both languages.
  'clientCompatibility.guidance.hiddify_import_access_link':
    'Import the RouteGate access link. Keep Hiddify routing/TUN settings compatible with the imported profile when using smart routing.',
  'clientCompatibility.guidance.v2rayn_routing_mode':
    'Use a routing mode that preserves DIRECT/VPN intent (for example a whitelist/custom rules mode rather than Global when DIRECT rules are required).',
  'clientCompatibility.guidance.v2rayn_tun_mode':
    'Enable TUN when system-wide routing is required and verify v2rayN DNS/routing rules do not override the RouteGate intent.',
  'clientCompatibility.guidance.v2rayng_standard_subscription':
    'v2rayNG is the Android member of the 2dust client family and supports standard subscription import.',
  'clientCompatibility.guidance.generic_standard_connection':
    'Standard VLESS/WireGuard/Shadowsocks/Hysteria2/MTProto connection material only. For RouteGate-managed routing, use Hiddify (full) or v2rayN (with client-side setup).',
  'clientCompatibility.limitation.v2rayn_no_routing_rules':
    'Standard URI subscriptions do not carry RouteGate sing-box routing rules; matching routing/DNS behavior must be configured in v2rayN.',
  'clientCompatibility.limitation.v2rayng_routing_not_validated':
    'RouteGate-managed Routing Profile enforcement is not yet validated on v2rayNG; only protocol-level connectivity is assumed.',
  'clientCompatibility.limitation.generic_no_routing_policy':
    'Only protocol-level connectivity is assumed for generic/unrecognized clients; RouteGate routing/DNS policy is not reproduced.',
  'clientCompatibility.limitation.protocol_validated_vless_only':
    'Full RouteGate smart routing delivery is currently validated for VLESS only; the selected protocol uses a connectivity fallback.',
  'clientCompatibility.limitation.protocol_no_share_link_format':
    'No validated client-specific subscription representation exists for the selected protocol; RouteGate falls back to protocol-native connectivity material.',
  'clientCompatibility.limitation.no_effective_protocol':
    'RouteGate cannot yet confirm this account’s effective protocol, so only protocol-level connectivity is assumed.',
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
  'clientCompatibility.guidance.hiddify_import_access_link':
    'Импортируйте ссылку доступа RouteGate. При использовании умной маршрутизации настройки Hiddify (роутинг/TUN) должны быть совместимы с импортированным профилем.',
  'clientCompatibility.guidance.v2rayn_routing_mode':
    'Используйте режим маршрутизации, сохраняющий намерение DIRECT/VPN (например, режим со своими правилами, а не Global, если требуются правила DIRECT).',
  'clientCompatibility.guidance.v2rayn_tun_mode':
    'Включите TUN, если нужна общесистемная маршрутизация, и проверьте, что правила DNS/маршрутизации v2rayN не переопределяют логику RouteGate.',
  'clientCompatibility.guidance.v2rayng_standard_subscription':
    'v2rayNG - Android-клиент семейства 2dust, поддерживает стандартный импорт подписки.',
  'clientCompatibility.guidance.generic_standard_connection':
    'Только стандартные данные подключения VLESS/WireGuard/Shadowsocks/Hysteria2/MTProto. Для управляемой RouteGate маршрутизации используйте Hiddify (полностью) или v2rayN (с ручной настройкой).',
  'clientCompatibility.limitation.v2rayn_no_routing_rules':
    'Стандартные URI-подписки не содержат правила маршрутизации sing-box от RouteGate; соответствующее поведение нужно настроить в v2rayN.',
  'clientCompatibility.limitation.v2rayng_routing_not_validated':
    'Применение Routing Profile RouteGate пока не подтверждено на v2rayNG; гарантируется только базовое подключение.',
  'clientCompatibility.limitation.generic_no_routing_policy':
    'Для универсальных/неизвестных клиентов гарантируется только базовое подключение; политика маршрутизации/DNS RouteGate не воспроизводится.',
  'clientCompatibility.limitation.protocol_validated_vless_only':
    'Полная умная маршрутизация RouteGate сейчас подтверждена только для VLESS; для выбранного протокола используется базовое подключение.',
  'clientCompatibility.limitation.protocol_no_share_link_format':
    'Для выбранного протокола нет подтверждённого клиентского представления подписки; RouteGate использует базовые данные подключения протокола.',
  'clientCompatibility.limitation.no_effective_protocol':
    'RouteGate пока не может подтвердить действующий протокол этого аккаунта, поэтому гарантируется только базовое подключение.',
};

export type ClientCompatibilityTranslationKey = keyof typeof clientCompatibilityEn;

const guidanceKeyByCode: Record<string, ClientCompatibilityTranslationKey> = {
  hiddify_import_access_link: 'clientCompatibility.guidance.hiddify_import_access_link',
  v2rayn_routing_mode: 'clientCompatibility.guidance.v2rayn_routing_mode',
  v2rayn_tun_mode: 'clientCompatibility.guidance.v2rayn_tun_mode',
  v2rayng_standard_subscription: 'clientCompatibility.guidance.v2rayng_standard_subscription',
  generic_standard_connection: 'clientCompatibility.guidance.generic_standard_connection',
};

const limitationKeyByCode: Record<string, ClientCompatibilityTranslationKey> = {
  v2rayn_no_routing_rules: 'clientCompatibility.limitation.v2rayn_no_routing_rules',
  v2rayng_routing_not_validated: 'clientCompatibility.limitation.v2rayng_routing_not_validated',
  generic_no_routing_policy: 'clientCompatibility.limitation.generic_no_routing_policy',
  protocol_validated_vless_only: 'clientCompatibility.limitation.protocol_validated_vless_only',
  protocol_no_share_link_format: 'clientCompatibility.limitation.protocol_no_share_link_format',
  no_effective_protocol: 'clientCompatibility.limitation.no_effective_protocol',
};

// Backend guidance/limitation codes are stable identifiers, not display
// text - these look up the corresponding i18n key so callers can pass the
// result straight to t(). An unrecognized code (e.g. an older frontend
// build against a newer backend) is dropped rather than rendered raw.
export function clientCompatibilityGuidanceKey(code: string): ClientCompatibilityTranslationKey | undefined {
  return guidanceKeyByCode[code];
}

export function clientCompatibilityLimitationKey(code: string): ClientCompatibilityTranslationKey | undefined {
  return limitationKeyByCode[code];
}
