export type Locale = 'ru' | 'en'

type Card = { title: string; text: string }

export type SiteContent = {
  nav: { product: string; openSource: string; docs: string; roadmap: string; changelog: string }
  action: { start: string; github: string }
  hero: { eyebrow: string; title: string; subtitle: string; description: string; note: string }
  dashboard: {
    overview: string; servers: string; accounts: string; clients: string; traffic: string
    infrastructure: string; healthy: string; map: string; online: string; activity: string
    applied: string; connected: string; latency: string
  }
  product: { eyebrow: string; title: string; intro: string; cards: Card[] }
  workflow: { eyebrow: string; title: string; steps: Card[] }
  source: { eyebrow: string; title: string; text: string; points: string[]; repository: string }
  deployment: { eyebrow: string; title: string; text: string; cards: Card[] }
  cta: { title: string; text: string }
  footer: { description: string; project: string; resources: string; legal: string; items: string[] }
}

export const content: Record<Locale, SiteContent> = {
  ru: {
    nav: { product: 'Продукт', openSource: 'Open Source', docs: 'Документация', roadmap: 'Дорожная карта', changelog: 'Changelog' },
    action: { start: 'Начать работу', github: 'Смотреть на GitHub' },
    hero: {
      eyebrow: 'OPEN SOURCE · AGPLv3-OR-LATER',
      title: 'RouteGate',
      subtitle: 'Открытая self-hosted платформа управления Linux VPN-инфраструктурой',
      description: 'Управляйте VPN-серверами, аккаунтами, маршрутными профилями и клиентским доступом из единого веб-интерфейса.',
      note: 'Разворачивайте самостоятельно. Сохраняйте контроль над инфраструктурой.',
    },
    dashboard: {
      overview: 'Обзор', servers: 'Серверы', accounts: 'VPN-аккаунты', clients: 'Клиенты', traffic: 'Трафик',
      infrastructure: 'VPN-инфраструктура', healthy: 'Все системы работают', map: 'Карта серверов',
      online: '5 из 5 онлайн', activity: 'Последняя активность', applied: 'Конфигурация применена',
      connected: 'Сервер подключён', latency: 'Средняя задержка',
    },
    product: {
      eyebrow: 'ЕДИНАЯ ПАНЕЛЬ',
      title: 'Всё необходимое для управления VPN-инфраструктурой',
      intro: 'От первого сервера до распределённой инфраструктуры — без разрозненных скриптов и панелей.',
      cards: [
        { title: 'Управление серверами', text: 'Подключайте и контролируйте Linux VPN-серверы из одной панели.' },
        { title: 'VPN-аккаунты', text: 'Создавайте доступ, управляйте жизненным циклом и статусами.' },
        { title: 'Маршрутные профили', text: 'Настраивайте Direct, VPN и Block правила для разных сценариев.' },
        { title: 'Доставка клиентам', text: 'Выдавайте конфигурации, QR-коды и ссылки подписки.' },
      ],
    },
    workflow: {
      eyebrow: 'КАК ЭТО РАБОТАЕТ', title: 'От управления до подключения',
      steps: [
        { title: 'Manager', text: 'Единая веб-панель и API.' },
        { title: 'Agent', text: 'Безопасное управление Linux-серверами.' },
        { title: 'VPN Core', text: 'Применение и проверка конфигураций.' },
        { title: 'Клиенты', text: 'Готовый доступ для пользователей.' },
      ],
    },
    source: {
      eyebrow: 'OPEN SOURCE', title: 'Открытый исходный код',
      text: 'RouteGate развивается открыто. Исходники Manager, Agent и Admin UI доступны на GitHub — проект можно собрать и развернуть в собственной инфраструктуре.',
      points: ['Исходники на GitHub', 'Самостоятельная сборка', 'Self-hosted развёртывание', 'AGPLv3-or-later'],
      repository: 'routegate/manager/config/apply.go',
    },
    deployment: {
      eyebrow: 'РАЗВЁРТЫВАНИЕ', title: 'Начните с одного сервера. Расширяйте по мере роста.',
      text: 'Одна и та же платформа для личной инфраструктуры, домашней лаборатории и небольших команд.',
      cards: [
        { title: 'Один VPS', text: 'Простой старт на Linux-сервере.' },
        { title: 'Несколько регионов', text: 'Единое управление распределёнными узлами.' },
        { title: 'Docker Compose', text: 'Предсказуемое развёртывание Manager.' },
        { title: 'systemd', text: 'Нативная эксплуатация Agent на Linux.' },
      ],
    },
    cta: { title: 'VPN-инфраструктура под вашим контролем', text: 'Разверните RouteGate и управляйте серверами, доступом и маршрутами из одной панели.' },
    footer: {
      description: 'Open-source self-hosted Linux VPN Management Platform.', project: 'Проект', resources: 'Ресурсы', legal: 'Открытый код',
      items: ['Продукт', 'Дорожная карта', 'Документация', 'GitHub', 'Releases', 'AGPLv3-or-later'],
    },
  },
  en: {
    nav: { product: 'Product', openSource: 'Open Source', docs: 'Docs', roadmap: 'Roadmap', changelog: 'Changelog' },
    action: { start: 'Get Started', github: 'View on GitHub' },
    hero: {
      eyebrow: 'OPEN SOURCE · AGPLv3-OR-LATER', title: 'RouteGate',
      subtitle: 'Open-source self-hosted Linux VPN Management Platform',
      description: 'Manage VPN servers, accounts, routing profiles, and client access from one clean web interface.',
      note: 'Deploy it yourself. Keep control of your infrastructure.',
    },
    dashboard: {
      overview: 'Overview', servers: 'Servers', accounts: 'VPN accounts', clients: 'Clients', traffic: 'Traffic',
      infrastructure: 'VPN infrastructure', healthy: 'All systems operational', map: 'Server map',
      online: '5 of 5 online', activity: 'Recent activity', applied: 'Configuration applied',
      connected: 'Server connected', latency: 'Average latency',
    },
    product: {
      eyebrow: 'ONE CONTROL PLANE', title: 'Everything you need to manage VPN infrastructure',
      intro: 'From the first server to distributed infrastructure — without disconnected scripts and panels.',
      cards: [
        { title: 'Server management', text: 'Connect and control Linux VPN servers from one place.' },
        { title: 'VPN accounts', text: 'Create access and manage lifecycle and status.' },
        { title: 'Routing profiles', text: 'Define Direct, VPN, and Block rules for each scenario.' },
        { title: 'Client delivery', text: 'Issue configurations, QR codes, and subscription links.' },
      ],
    },
    workflow: {
      eyebrow: 'HOW IT WORKS', title: 'From control to connection',
      steps: [
        { title: 'Manager', text: 'A single web interface and API.' },
        { title: 'Agent', text: 'Safe control of Linux servers.' },
        { title: 'VPN Core', text: 'Validated configuration apply.' },
        { title: 'Clients', text: 'Ready access for users.' },
      ],
    },
    source: {
      eyebrow: 'OPEN SOURCE', title: 'Open-source by design',
      text: 'RouteGate is developed in the open. The Manager, Agent, and Admin UI are available on GitHub, ready to build and run in your own infrastructure.',
      points: ['Source on GitHub', 'Build from source', 'Self-hosted deployment', 'AGPLv3-or-later'], repository: 'routegate/manager/config/apply.go',
    },
    deployment: {
      eyebrow: 'DEPLOYMENT', title: 'Start with one server. Expand as you grow.',
      text: 'The same platform for personal infrastructure, home labs, and small teams.',
      cards: [
        { title: 'One VPS', text: 'A simple start on a Linux server.' },
        { title: 'Multiple regions', text: 'One view across distributed nodes.' },
        { title: 'Docker Compose', text: 'Predictable Manager deployment.' },
        { title: 'systemd', text: 'Native Agent operation on Linux.' },
      ],
    },
    cta: { title: 'Your VPN infrastructure. Under your control.', text: 'Deploy RouteGate and manage servers, access, and routing from one control plane.' },
    footer: {
      description: 'Open-source self-hosted Linux VPN Management Platform.', project: 'Project', resources: 'Resources', legal: 'Open source',
      items: ['Product', 'Roadmap', 'Documentation', 'GitHub', 'Releases', 'AGPLv3-or-later'],
    },
  },
}
