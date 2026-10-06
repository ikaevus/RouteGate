export type Locale = 'ru' | 'en'

type Card = { title: string; text: string }
type RoadmapColumn = { title: string; items: string[] }
type FAQItem = { question: string; answer: string }

export type SiteContent = {
  nav: { product: string; openSource: string; docs: string; roadmap: string; changelog: string }
  action: { start: string; github: string; installGuide: string; copy: string; copied: string }
  hero: { eyebrow: string; title: string; subtitle: string; description: string; note: string }
  dashboard: {
    overview: string; servers: string; accounts: string; clients: string; traffic: string
    infrastructure: string; healthy: string; map: string; online: string; activity: string
    applied: string; connected: string; latency: string
  }
  product: { eyebrow: string; title: string; intro: string; cards: Card[] }
  workflow: { eyebrow: string; title: string; steps: Card[] }
  source: { eyebrow: string; title: string; text: string; points: string[]; repository: string; realCode: string }
  deployment: {
    eyebrow: string; title: string; text: string; cards: Card[]
    commandLabel: string; commandTitle: string; commandNote: string
  }
  roadmap: { eyebrow: string; title: string; intro: string; columns: RoadmapColumn[] }
  faq: {
    eyebrow: string; title: string; intro: string; items: FAQItem[]
    securityTitle: string; securityText: string; securityLink: string
  }
  cta: { title: string; text: string }
  footer: { description: string; project: string; resources: string; legal: string; items: string[] }
}

export const content: Record<Locale, SiteContent> = {
  ru: {
    nav: { product: 'Продукт', openSource: 'Открытый код', docs: 'Документация', roadmap: 'Дорожная карта', changelog: 'История изменений' },
    action: { start: 'Начать работу', github: 'Смотреть на GitHub', installGuide: 'Руководство по установке', copy: 'Копировать', copied: 'Скопировано' },
    hero: {
      eyebrow: 'ОТКРЫТЫЙ КОД · AGPLv3-OR-LATER',
      title: 'RouteGate',
      subtitle: 'Управляйте Linux VPN-инфраструктурой из одной точки',
      description: 'Открытая платформа для самостоятельного управления VPN-узлами, аккаунтами, устройствами, маршрутизацией и пользовательским доступом.',
      note: 'Разворачивайте самостоятельно. Сохраняйте контроль над инфраструктурой и данными.',
    },
    dashboard: {
      overview: 'Обзор', servers: 'Серверы', accounts: 'VPN-аккаунты', clients: 'Клиенты', traffic: 'Трафик',
      infrastructure: 'VPN-инфраструктура', healthy: 'Все системы работают', map: 'Карта серверов',
      online: '6 из 6 онлайн', activity: 'Последняя активность', applied: 'Конфигурация применена',
      connected: 'Сервер подключён', latency: 'Средняя задержка',
    },
    product: {
      eyebrow: 'ЕДИНАЯ ПАНЕЛЬ УПРАВЛЕНИЯ',
      title: 'От VPN-узла до пользовательского доступа',
      intro: 'RouteGate управляет жизненным циклом инфраструктуры и клиентского доступа, а не только генерирует конфигурации.',
      cards: [
        { title: 'Управляемые VPN-узлы', text: 'Подключайте локальные и удалённые Linux-узлы через RouteGate Agent с явным состоянием и проверками.' },
        { title: 'Несколько протоколов', text: 'VLESS / Reality, WireGuard, Hysteria2, Shadowsocks 2022 и MTProto / FakeTLS в одной модели управления.' },
        { title: 'Доступ и устройства', text: 'VPN-аккаунты, отдельные устройства, подписки, QR-коды, отзыв и ротация доступа.' },
        { title: 'Маршрутизация и доставка', text: 'Профили Direct, VPN и Block с доставкой под возможности конкретного VPN-клиента.' },
      ],
    },
    workflow: {
      eyebrow: 'КАК ЭТО РАБОТАЕТ',
      title: 'Безопасный путь от узла до клиента',
      steps: [
        { title: 'Manager', text: 'Единая панель, состояние и управляемые рабочие процессы.' },
        { title: 'Agent', text: 'Ограниченные операции на Linux-узлах без произвольного удалённого shell.' },
        { title: 'VPN-компонент', text: 'Установка, рендер, проверка, применение и контроль состояния.' },
        { title: 'Доступ пользователя', text: 'Устройства, подписки, QR и инструкции без инфраструктурных деталей.' },
      ],
    },
    source: {
      eyebrow: 'ОТКРЫТЫЙ КОД',
      title: 'Код, который можно проверить',
      text: 'Manager, Agent, панель администратора и Портал пользователя развиваются в открытом репозитории. На сайте показан настоящий фрагмент текущего серверного кода, а не декоративный пример.',
      points: ['Исходники на GitHub', 'Самостоятельная сборка', 'Самостоятельное развёртывание', 'AGPLv3-or-later'],
      repository: 'backend/internal/configs/lifecycle.go',
      realCode: 'реальный файл',
    },
    deployment: {
      eyebrow: 'УСТАНОВКА',
      title: 'Разверните RouteGate на чистом Ubuntu VPS',
      text: 'Публичная установка на чистый хост использует Ubuntu 24.04 LTS, PostgreSQL, systemd, nginx и HTTPS. VPN-компоненты устанавливаются позже через управляемый сценарий.',
      cards: [
        { title: 'Проверяемая установка', text: 'Скачайте install.sh, просмотрите его и только затем запускайте с повышенными правами.' },
        { title: 'Платформа сначала', text: 'Установщик поднимает Manager, Agent, PostgreSQL, nginx и защищённую точку входа.' },
        { title: 'VPN-компонент по требованию', text: 'VLESS, WireGuard, Hysteria2, Shadowsocks или MTProto устанавливаются после выбора протокола.' },
        { title: 'Безопасный первый вход', text: 'Одноразовая ссылка /setup создаёт первого администратора без заводского пароля.' },
      ],
      commandLabel: 'БЫСТРЫЙ СТАРТ',
      commandTitle: 'Скачать, проверить и установить v0.1.0',
      commandNote: 'Для новой версии замените VERSION на нужный release tag. Не запускайте удалённый скрипт вслепую через curl | sudo bash.',
    },
    roadmap: {
      eyebrow: 'ДОРОЖНАЯ КАРТА',
      title: 'Куда развивается RouteGate',
      intro: 'Это продуктовая карта направлений, а не обещание дат. Уже работающие возможности отделены от текущего усиления и следующих задач.',
      columns: [
        { title: 'Уже работает', items: [
          'Управляемые VLESS / Reality, WireGuard, Hysteria2, Shadowsocks 2022 и MTProto / FakeTLS.',
          'Доступ и устройства, клиентские подписки, QR и Портал пользователя.',
          'Профили маршрутизации и доставка с учётом возможностей конкретного клиента.',
          'Проверяемые обновления платформы и последовательное обновление VPN-узлов.',
        ]},
        { title: 'Сейчас усиливаем', items: [
          'Непрерывность подписок и безопасный перенос аккаунтов между узлами.',
          'Совместимость VPN-клиентов и честное отображение ограничений маршрутизации.',
          'Надёжность, восстановление, обслуживание и эксплуатационные сценарии.',
          'Документацию, установку и публичный сайт в соответствии с текущим продуктом.',
        ]},
        { title: 'Дальше', items: [
          'Более безопасная автоматизация выбора узлов и аварийного переключения без скрытых переносов.',
          'Более широкая проверка протоколов, клиентов и топологий, приближённых к рабочим.',
          'Дальнейшее развитие Портала пользователя и сценариев самообслуживания.',
          'Масштабирование архитектуры только там, где оно оправдано реальной нагрузкой.',
        ]},
      ],
    },
    faq: {
      eyebrow: 'FAQ',
      title: 'Коротко о требованиях и границах',
      intro: 'То, что обычно нужно понять до установки и до оценки RouteGate как платформы.',
      items: [
        { question: 'Какие протоколы управляются сейчас?', answer: 'В текущей модели RouteGate есть управляемые семейства VLESS / Reality, WireGuard, Hysteria2, Shadowsocks 2022 и MTProto / FakeTLS.' },
        { question: 'Что нужно для первого сервера?', answer: 'Чистый Ubuntu 24.04 LTS VPS на amd64, DNS-имя, доступ root или sudo и доступные TCP-порты 80 и 443 для Manager.' },
        { question: 'RouteGate — это VPN-сервис для конечного пользователя?', answer: 'Нет. Это платформа для самостоятельного развёртывания и управления собственной VPN-инфраструктурой. Оператор сам разворачивает и контролирует серверы и данные.' },
        { question: 'RouteGate скрывает ограничения клиента?', answer: 'Нет. Подключение само по себе не считается доказательством поддержки маршрутизации. Интерфейс должен показывать совместимость и известные ограничения явно.' },
      ],
      securityTitle: 'Нашли уязвимость?',
      securityText: 'Не публикуйте чувствительные детали в открытом issue. Используйте приватный процесс раскрытия, описанный в SECURITY.md.',
      securityLink: 'Политика безопасности',
    },
    cta: { title: 'VPN-инфраструктура под вашим контролем', text: 'Начните с проверяемой установки и дальше двигайтесь по управляемому сценарию RouteGate.' },
    footer: {
      description: 'Открытая платформа для самостоятельного управления Linux VPN-инфраструктурой.',
      project: 'Проект', resources: 'Ресурсы', legal: 'Открытый код и безопасность',
      items: ['Продукт', 'Дорожная карта', 'Документация', 'GitHub', 'История изменений', 'Безопасность', 'AGPLv3-or-later'],
    },
  },
  en: {
    nav: { product: 'Product', openSource: 'Open Source', docs: 'Docs', roadmap: 'Roadmap', changelog: 'Changelog' },
    action: { start: 'Get Started', github: 'View on GitHub', installGuide: 'Installation guide', copy: 'Copy', copied: 'Copied' },
    hero: {
      eyebrow: 'OPEN SOURCE · AGPLv3-OR-LATER',
      title: 'RouteGate',
      subtitle: 'Control your Linux VPN infrastructure',
      description: 'An open-source self-hosted control plane for VPN nodes, accounts, devices, routing, and user access.',
      note: 'Deploy it yourself. Keep control of your infrastructure and data.',
    },
    dashboard: {
      overview: 'Overview', servers: 'Servers', accounts: 'VPN accounts', clients: 'Clients', traffic: 'Traffic',
      infrastructure: 'VPN infrastructure', healthy: 'All systems operational', map: 'Server map',
      online: '6 of 6 online', activity: 'Recent activity', applied: 'Configuration applied',
      connected: 'Server connected', latency: 'Average latency',
    },
    product: {
      eyebrow: 'ONE CONTROL PLANE',
      title: 'From VPN nodes to user access',
      intro: 'RouteGate manages infrastructure and client-access lifecycle rather than only generating configuration files.',
      cards: [
        { title: 'Managed VPN nodes', text: 'Connect local and remote Linux nodes through RouteGate Agent with explicit state and validation.' },
        { title: 'Multiple protocols', text: 'VLESS / Reality, WireGuard, Hysteria2, Shadowsocks 2022, and MTProto / FakeTLS in one management model.' },
        { title: 'Access & Devices', text: 'VPN accounts, per-device access, subscriptions, QR codes, revocation, and rotation.' },
        { title: 'Routing & delivery', text: 'Direct, VPN, and Block profiles delivered according to each VPN client’s capabilities.' },
      ],
    },
    workflow: {
      eyebrow: 'HOW IT WORKS',
      title: 'A safe path from node to client',
      steps: [
        { title: 'Manager', text: 'One control plane for state and guided operational workflows.' },
        { title: 'Agent', text: 'Allow-listed Linux node operations without arbitrary remote shell authority.' },
        { title: 'VPN runtime', text: 'Install, render, validate, apply, and verify managed runtime state.' },
        { title: 'User access', text: 'Devices, subscriptions, QR, and guidance without infrastructure internals.' },
      ],
    },
    source: {
      eyebrow: 'OPEN SOURCE',
      title: 'Code you can inspect',
      text: 'Manager, Agent, Admin UI, and User Portal are developed in the public repository. The website now shows a real excerpt from the current backend instead of decorative sample code.',
      points: ['Source on GitHub', 'Build from source', 'Self-hosted deployment', 'AGPLv3-or-later'],
      repository: 'backend/internal/configs/lifecycle.go',
      realCode: 'real source file',
    },
    deployment: {
      eyebrow: 'INSTALLATION',
      title: 'Deploy RouteGate on a clean Ubuntu VPS',
      text: 'The public clean-host path uses Ubuntu 24.04 LTS, PostgreSQL, systemd, nginx, and HTTPS. VPN runtimes are installed later through the managed workflow.',
      cards: [
        { title: 'Reviewable install', text: 'Download install.sh, inspect it, and only then run it with elevated privileges.' },
        { title: 'Platform first', text: 'The installer brings up Manager, Agent, PostgreSQL, nginx, and the protected entry point.' },
        { title: 'Runtime on demand', text: 'VLESS, WireGuard, Hysteria2, Shadowsocks, or MTProto runtime is installed after protocol selection.' },
        { title: 'Secure first access', text: 'A single-use /setup link creates the first administrator without a factory password.' },
      ],
      commandLabel: 'QUICK START',
      commandTitle: 'Download, review, and install v0.1.0',
      commandNote: 'For a newer release, replace VERSION with the desired release tag. Do not blindly pipe a remote script into sudo bash.',
    },
    roadmap: {
      eyebrow: 'ROADMAP',
      title: 'Where RouteGate is heading',
      intro: 'This is a product-direction map, not a promise of dates. Shipped capability is separated from current hardening and later work.',
      columns: [
        { title: 'Shipped', items: [
          'Managed VLESS / Reality, WireGuard, Hysteria2, Shadowsocks 2022, and MTProto / FakeTLS.',
          'Access & Devices, client subscriptions, QR delivery, and User Portal.',
          'Routing profiles and capability-aware client delivery.',
          'Verified platform updates and ordered VPN-node rollout.',
        ]},
        { title: 'Current focus', items: [
          'Subscription continuity and safe account transfer between nodes.',
          'VPN-client compatibility and explicit routing-delivery limitations.',
          'Reliability, recovery, maintenance, and operational workflows.',
          'Documentation, installation, and public-site alignment with the current product.',
        ]},
        { title: 'Next', items: [
          'Safer node-selection and failover automation without hidden account moves.',
          'Broader protocol, client, and production-like topology validation.',
          'Continued User Portal and self-service development.',
          'Architecture scaling only where real operational pressure justifies it.',
        ]},
      ],
    },
    faq: {
      eyebrow: 'FAQ',
      title: 'Requirements and boundaries',
      intro: 'The essentials to understand before installing RouteGate or evaluating the platform.',
      items: [
        { question: 'Which protocols are managed today?', answer: 'The current RouteGate model includes managed VLESS / Reality, WireGuard, Hysteria2, Shadowsocks 2022, and MTProto / FakeTLS families.' },
        { question: 'What does the first server require?', answer: 'A clean Ubuntu 24.04 LTS amd64 VPS, a DNS hostname, root or working sudo access, and reachable TCP ports 80 and 443 for Manager.' },
        { question: 'Is RouteGate a consumer VPN service?', answer: 'No. RouteGate is a self-hosted management platform for infrastructure you operate and control.' },
        { question: 'Does RouteGate hide client limitations?', answer: 'No. Successful connectivity is not presented as proof of routing-policy support. Compatibility state and known limitations should remain explicit.' },
      ],
      securityTitle: 'Found a vulnerability?',
      securityText: 'Do not post sensitive details in a public issue. Use the private disclosure process described in SECURITY.md.',
      securityLink: 'Security policy',
    },
    cta: { title: 'Your VPN infrastructure. Under your control.', text: 'Start with a reviewable installation and continue through RouteGate’s guided workflow.' },
    footer: {
      description: 'Open-source self-hosted Linux VPN infrastructure management.',
      project: 'Project', resources: 'Resources', legal: 'Open source & security',
      items: ['Product', 'Roadmap', 'Documentation', 'GitHub', 'Changelog', 'Security', 'AGPLv3-or-later'],
    },
  },
}
