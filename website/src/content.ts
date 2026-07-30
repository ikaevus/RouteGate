export type Locale = 'ru' | 'en'

export const content = {
  ru: {
    nav: ['Продукт', 'Open Source', 'Документация', 'Дорожная карта', 'Changelog'],
    hero: {
      eyebrow: 'OPEN SOURCE · AGPLv3-or-later',
      title: 'RouteGate',
      subtitle: 'Открытая self-hosted платформа управления Linux VPN-инфраструктурой',
      description: 'Управляйте VPN-серверами, аккаунтами, маршрутными профилями и клиентским доступом из единого веб-интерфейса. Разворачивайте RouteGate самостоятельно и сохраняйте контроль над своей инфраструктурой.',
      primary: 'Начать работу',
      secondary: 'Смотреть на GitHub',
      trust: 'Открытый исходный код. Защищённый бренд. Официальные сборки и поддержка — позже.',
    },
    sections: {
      productTitle: 'Всё необходимое для управления VPN-инфраструктурой',
      featuresTitle: 'Ключевые возможности',
      workflowTitle: 'Как это работает',
      sourceTitle: 'Открытый исходный код',
      sourceText: 'RouteGate разрабатывается как open-source проект. Исходный код Manager, Agent и связанных компонентов доступен на GitHub. Проект можно изучать, собирать самостоятельно и разворачивать в собственной инфраструктуре.',
      deployTitle: 'Варианты развёртывания',
      ctaTitle: 'Возьмите VPN-инфраструктуру под свой контроль',
      ctaText: 'Разверните RouteGate и управляйте доступом, маршрутизацией и конфигурациями из одной панели.',
    },
  },
  en: {
    nav: ['Product', 'Open Source', 'Docs', 'Roadmap', 'Changelog'],
    hero: {
      eyebrow: 'OPEN SOURCE · AGPLv3-or-later',
      title: 'RouteGate',
      subtitle: 'Open-source self-hosted Linux VPN Management Platform',
      description: 'Manage VPN servers, accounts, routing profiles, and client access from one clean web interface. Deploy RouteGate yourself and keep control of your infrastructure.',
      primary: 'Get Started',
      secondary: 'View on GitHub',
      trust: 'Open source. Protected brand. Official builds and support later.',
    },
    sections: {
      productTitle: 'Everything needed to manage VPN infrastructure',
      featuresTitle: 'Core capabilities',
      workflowTitle: 'How it works',
      sourceTitle: 'Open source',
      sourceText: 'RouteGate is developed as an open-source project. The Manager, Agent, and related components are available on GitHub. You can study the project, build it from source, and run it in your own infrastructure.',
      deployTitle: 'Deployment options',
      ctaTitle: 'Take control of your VPN infrastructure',
      ctaText: 'Deploy RouteGate and manage access, routing, and configuration from one control plane.',
    },
  },
} as const
