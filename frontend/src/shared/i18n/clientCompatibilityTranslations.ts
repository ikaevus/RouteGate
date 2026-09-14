export const clientCompatibilityEn = {
  'clientCompatibility.title': 'Client compatibility',
  'clientCompatibility.full': 'Full RouteGate',
  'clientCompatibility.setup': 'Compatible',
  'clientCompatibility.connectionOnly': 'Basic connection',
  'clientCompatibility.hiddify': 'Hiddify',
  'clientCompatibility.happ': 'HAPP',
  'clientCompatibility.copySubscriptionUrl': 'Copy link',
  'clientCompatibility.close': 'Close',
  'clientCompatibility.copied': 'Copied',
  // Guidance/limitation copy, keyed by the stable codes the backend returns
  // in ClientCompatibilityAssessment.guidanceCodes/limitationCodes. The
  // backend never sends localized prose; this is the only place these
  // messages are written out, in both languages.
  'clientCompatibility.guidance.hiddify_import_access_link':
    'Import the RouteGate access link as a standard Hiddify subscription. The same portable link can also be imported by other compatible clients.',
  'clientCompatibility.guidance.happ_standard_subscription':
    'Import the RouteGate access link as a standard HAPP subscription. VLESS/Reality and Shadowsocks connection delivery are manually validated. Provider-managed RouteGate routing is temporarily disabled for HAPP after a failed real-device safety acceptance test.',
  'clientCompatibility.guidance.v2rayn_import_subscription':
    'In v2rayN, open Subscription group → Subscription group settings → Add, paste this link into URL, and save. Then open Subscription group → Update subscription without proxy (or via proxy if RouteGate is only reachable through the current VPN). “Import Share Links from clipboard” only registers the HTTPS subscription; it does not download its servers immediately.',
  'clientCompatibility.guidance.v2rayn_routing_mode':
    'Use a routing mode that preserves DIRECT/VPN intent (for example a whitelist/custom rules mode rather than Global when DIRECT rules are required).',
  'clientCompatibility.guidance.v2rayn_tun_mode':
    'Enable TUN when system-wide routing is required and verify v2rayN DNS/routing rules do not override the RouteGate intent.',
  'clientCompatibility.guidance.v2rayng_standard_subscription':
    'v2rayNG is the Android member of the 2dust client family and supports standard subscription import.',
  'clientCompatibility.guidance.generic_standard_connection':
    'Standard VLESS/WireGuard/Shadowsocks/Hysteria2/MTProto connection material only. Routing and DNS behavior must be configured in the selected client.',
  'clientCompatibility.limitation.hiddify_routing_client_local':
    'The standard access link carries connection profiles, not the RouteGate Routing Profile. Configure routing, DNS, and TUN behavior in Hiddify.',
  'clientCompatibility.limitation.happ_routing_not_validated':
    'RouteGate does not currently push DIRECT/VPN/BLOCK policy into HAPP. A real-device acceptance attempt disrupted client traffic, so HAPP remains a validated connection client while managed routing is disabled pending a safe implementation.',
  'clientCompatibility.limitation.happ_protocol_not_validated':
    'This HAPP protocol path has not yet completed RouteGate real-client acceptance; use it as a connectivity fallback until validated.',
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
  'clientCompatibility.connectionOnly': 'Базовое подключение',
  'clientCompatibility.hiddify': 'Hiddify',
  'clientCompatibility.happ': 'HAPP',
  'clientCompatibility.copySubscriptionUrl': 'Копировать ссылку',
  'clientCompatibility.close': 'Закрыть',
  'clientCompatibility.copied': 'Скопировано',
  'clientCompatibility.guidance.hiddify_import_access_link':
    'Импортируйте ссылку доступа RouteGate как стандартную подписку Hiddify. Эту же переносимую ссылку можно импортировать и в другие совместимые клиенты.',
  'clientCompatibility.guidance.happ_standard_subscription':
    'Импортируйте ссылку доступа RouteGate как стандартную подписку HAPP. Подключение VLESS/Reality и Shadowsocks проверено вручную. Управляемая маршрутизация RouteGate для HAPP временно отключена после неуспешной проверки безопасности на реальном устройстве.',
  'clientCompatibility.guidance.v2rayn_import_subscription':
    'В v2rayN откройте «Группа подписки» → «Настройки группы подписки» → «Добавить», вставьте эту ссылку в поле URL и сохраните. Затем выберите «Группа подписки» → «Обновить подписку без прокси» (или «с прокси», если RouteGate доступен только через текущий VPN). Пункт «Импорт массива URL из буфера обмена» только регистрирует HTTPS-подписку и не загружает серверы сразу.',
  'clientCompatibility.guidance.v2rayn_routing_mode':
    'Используйте режим маршрутизации, сохраняющий намерение DIRECT/VPN (например, режим со своими правилами, а не Global, если требуются правила DIRECT).',
  'clientCompatibility.guidance.v2rayn_tun_mode':
    'Включите TUN, если нужна общесистемная маршрутизация, и проверьте, что правила DNS/маршрутизации v2rayN не переопределяют логику RouteGate.',
  'clientCompatibility.guidance.v2rayng_standard_subscription':
    'v2rayNG - Android-клиент семейства 2dust, поддерживает стандартный импорт подписки.',
  'clientCompatibility.guidance.generic_standard_connection':
    'Только стандартные данные подключения VLESS/WireGuard/Shadowsocks/Hysteria2/MTProto. Маршрутизацию и DNS нужно настроить в выбранном клиенте.',
  'clientCompatibility.limitation.hiddify_routing_client_local':
    'Стандартная ссылка содержит профили подключения, но не Routing Profile RouteGate. Настройте маршрутизацию, DNS и режим TUN в Hiddify.',
  'clientCompatibility.limitation.happ_routing_not_validated':
    'RouteGate сейчас не отправляет правила DIRECT/VPN/BLOCK в HAPP. Реальная проверка на устройстве нарушила клиентский трафик, поэтому HAPP остаётся проверенным клиентом подключения, а управляемая маршрутизация отключена до безопасной реализации.',
  'clientCompatibility.limitation.happ_protocol_not_validated':
    'Этот протокольный сценарий HAPP ещё не прошёл реальную клиентскую проверку RouteGate; до проверки он считается резервным вариантом базового подключения.',
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
  happ_standard_subscription: 'clientCompatibility.guidance.happ_standard_subscription',
  v2rayn_import_subscription: 'clientCompatibility.guidance.v2rayn_import_subscription',
  v2rayn_routing_mode: 'clientCompatibility.guidance.v2rayn_routing_mode',
  v2rayn_tun_mode: 'clientCompatibility.guidance.v2rayn_tun_mode',
  v2rayng_standard_subscription: 'clientCompatibility.guidance.v2rayng_standard_subscription',
  generic_standard_connection: 'clientCompatibility.guidance.generic_standard_connection',
};

const limitationKeyByCode: Record<string, ClientCompatibilityTranslationKey> = {
  hiddify_routing_client_local: 'clientCompatibility.limitation.hiddify_routing_client_local',
  happ_routing_not_validated: 'clientCompatibility.limitation.happ_routing_not_validated',
  happ_protocol_not_validated: 'clientCompatibility.limitation.happ_protocol_not_validated',
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
