import { useState } from 'react'
import { content, type Locale } from './content'

const productCards = [
  ['Управление серверами', 'Добавляйте, контролируйте и обслуживайте Linux VPN-серверы.'],
  ['VPN-аккаунты', 'Создавайте, организуйте и отключайте учётные записи.'],
  ['Маршрутные профили', 'Определяйте правила Direct, VPN и Block.'],
  ['Доставка клиентам', 'Выдавайте конфигурации, QR-коды и ссылки подписки.'],
]

const workflow = [
  ['Manager', 'Управляет серверами и конфигурациями.'],
  ['Аккаунты', 'Создание и управление VPN-доступом.'],
  ['Профили', 'Определение правил и маршрутов.'],
  ['Клиенты', 'Выдача конфигураций и подключение.'],
]

const deployment = [
  ['Один VPS', 'Быстрый старт на одном Linux-сервере.'],
  ['Несколько серверов', 'Масштабирование инфраструктуры по мере роста.'],
  ['Командная работа', 'Совместное управление аккаунтами и профилями.'],
  ['Self-hosted контроль', 'Ваши данные. Ваш стек. Ваши правила.'],
]

function Logo() {
  return <img className="logo" src="/routegate-logo.svg" alt="RouteGate" />
}

function DashboardPreview() {
  return (
    <div className="dashboard-shell" aria-label="RouteGate dashboard preview">
      <aside className="dashboard-sidebar">
        <div className="mini-brand"><span className="mark">RG</span><strong>RouteGate</strong></div>
        {['Обзор', 'Серверы', 'VPN-аккаунты', 'Клиенты', 'Трафик'].map((item, index) => (
          <span className={index === 0 ? 'active' : ''} key={item}>{item}</span>
        ))}
      </aside>
      <div className="dashboard-content">
        <div className="dashboard-heading"><div><small>Обзор</small><h3>VPN-инфраструктура</h3></div><span className="status">Все системы работают</span></div>
        <div className="stats">
          <div><span>Серверы</span><strong>5</strong></div>
          <div><span>VPN-аккаунты</span><strong>48</strong></div>
          <div><span>Клиенты</span><strong>32</strong></div>
          <div><span>Трафик</span><strong>2.4 TB</strong></div>
        </div>
        <div className="map-card">
          <div className="map-title"><strong>Карта серверов</strong><span>5 онлайн</span></div>
          <div className="world-map" aria-hidden="true">
            <span className="continent america" /><span className="continent europe" /><span className="continent asia" /><span className="continent australia" />
            <i className="pin p1" /><i className="pin p2" /><i className="pin p3" /><i className="pin p4" /><i className="pin p5" />
          </div>
        </div>
        <div className="activity"><strong>Последняя активность</strong><span>de-fra-01 · конфигурация применена</span><span>us-nyc-01 · сервер подключён</span></div>
      </div>
    </div>
  )
}

export function App() {
  const [locale, setLocale] = useState<Locale>('ru')
  const t = content[locale]

  return (
    <div className="site">
      <header className="header container">
        <a href="#top" className="brand"><Logo /></a>
        <nav>{t.nav.map((item) => <a href={`#${item.toLowerCase().replaceAll(' ', '-')}`} key={item}>{item}</a>)}</nav>
        <div className="header-actions"><button className="locale" onClick={() => setLocale(locale === 'ru' ? 'en' : 'ru')}>{locale.toUpperCase()}</button><a className="button primary" href="#start">{t.hero.primary}</a></div>
      </header>

      <main id="top">
        <section className="hero container">
          <div className="hero-copy">
            <span className="eyebrow">{t.hero.eyebrow}</span>
            <h1>{t.hero.title}</h1>
            <h2>{t.hero.subtitle}</h2>
            <p>{t.hero.description}</p>
            <div className="actions"><a className="button primary" href="#start">{t.hero.primary}</a><a className="button ghost" href="https://github.com/ikaevus/RouteGate">{t.hero.secondary}</a></div>
            <small className="trust">{t.hero.trust}</small>
          </div>
          <DashboardPreview />
        </section>

        <section className="section container" id="продукт"><h2>{t.sections.productTitle}</h2><div className="grid four">{productCards.map(([title, text]) => <article className="card" key={title}><span className="icon">✦</span><h3>{title}</h3><p>{text}</p></article>)}</div></section>

        <section className="section split" id="open-source"><div className="container split-inner"><div><span className="eyebrow">OPEN SOURCE</span><h2>{t.sections.sourceTitle}</h2><p>{t.sections.sourceText}</p><ul><li>Исходный код на GitHub</li><li>Самостоятельная сборка</li><li>Self-hosted развёртывание</li><li>AGPLv3-or-later</li></ul></div><pre className="code-card"><code><span>type</span> RouteProfile <span>struct</span> {'{'}
  Name  string
  Mode  string
  Rules []RouteRule
{'}'}

<span>func</span> ApplyConfig(ctx context.Context) error {'{'}
  // render → validate → stage → apply
  return nil
{'}'}</code></pre></div></section>

        <section className="section container"><h2>{t.sections.workflowTitle}</h2><div className="workflow">{workflow.map(([title, text], index) => <article key={title}><b>{index + 1}</b><h3>{title}</h3><p>{text}</p></article>)}</div></section>

        <section className="section container"><h2>{t.sections.deployTitle}</h2><div className="grid four">{deployment.map(([title, text]) => <article className="card" key={title}><h3>{title}</h3><p>{text}</p></article>)}</div></section>

        <section className="cta container" id="start"><div><h2>{t.sections.ctaTitle}</h2><p>{t.sections.ctaText}</p></div><div className="actions"><a className="button primary" href="#docs">{t.hero.primary}</a><a className="button ghost" href="https://github.com/ikaevus/RouteGate">{t.hero.secondary}</a></div></section>
      </main>

      <footer className="footer container"><Logo /><p>Open-source self-hosted Linux VPN Management Platform.</p><span>© 2026 RouteGate · AGPLv3-or-later</span></footer>
    </div>
  )
}
