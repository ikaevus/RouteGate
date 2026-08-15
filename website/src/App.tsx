import { useEffect, useMemo, useState } from 'react'
import { content, type Locale, type SiteContent } from './content'

const githubUrl = 'https://github.com/ikaevus/RouteGate'
const docsUrl = `${githubUrl}/tree/main/docs`
const releasesUrl = `${githubUrl}/releases`
const licenseUrl = `${githubUrl}/blob/main/LICENSE`
const assetUrl = (path: string) => `${import.meta.env.BASE_URL}${path.replace(/^\//, '')}`

function Brand({ compact = false }: { compact?: boolean }) {
  return <span className={`brand ${compact ? 'brand--compact' : ''}`}><img src={assetUrl('routegate-symbol.svg')} alt="" /><span>RouteGate</span></span>
}

function Icon({ name }: { name: 'server' | 'account' | 'route' | 'client' }) {
  const paths = {
    server: <><rect x="4" y="5" width="16" height="6" rx="2" /><rect x="4" y="13" width="16" height="6" rx="2" /><path d="M8 8h.01M8 16h.01M12 8h5M12 16h5" /></>,
    account: <><circle cx="12" cy="8" r="3" /><path d="M5 20c.6-4 2.8-6 7-6s6.4 2 7 6" /></>,
    route: <><circle cx="6" cy="17" r="2" /><circle cx="18" cy="7" r="2" /><path d="M8 17h2a4 4 0 0 0 4-4v-2a4 4 0 0 1 4-4" /></>,
    client: <><rect x="3" y="4" width="18" height="13" rx="2" /><path d="M8 21h8M12 17v4" /></>,
  }
  return <svg viewBox="0 0 24 24" aria-hidden="true">{paths[name]}</svg>
}

function WorldMap({ t }: { t: SiteContent['dashboard'] }) {
  return (
    <div className="map-widget">
      <div className="widget-heading"><div><strong>{t.map}</strong><span>1 / 1 online</span></div><button type="button" tabIndex={-1} aria-hidden="true">•••</button></div>
      <div className="world-map">
        <img src={assetUrl('world-map-natural-earth.svg')} alt="" />
        <span className="server-marker" style={{ left: '27.5%', top: '39%' }}><i /><em>us.routegate.org</em></span>
      </div>
      <div className="map-status"><span><i />{t.online}</span><a href="https://www.naturalearthdata.com/" target="_blank" rel="noreferrer">Natural Earth · 1:110m</a></div>
    </div>
  )
}

function DashboardPreview({ t }: { t: SiteContent['dashboard'] }) {
  const isEnglish = t.overview === 'Overview'
  const nav = [t.overview, t.servers, t.accounts, isEnglish ? 'Configuration / Deploy' : 'Конфигурация / Deploy', isEnglish ? 'Routing profiles' : 'Маршрутные профили', 'User portal']
  const stats = isEnglish
    ? [['Managed servers', '1 / 1', 'online'], ['Agents', '1 / 1', 'healthy'], ['VPN Core', 'sing-box', 'active'], ['Deploy', 'Applied', 'healthy']]
    : [['Серверы', '1 / 1', 'онлайн'], ['Агенты', '1 / 1', 'работают'], ['VPN Core', 'sing-box', 'active'], ['Deploy', 'Применён', 'healthy']]

  return (
    <div className="dashboard-wrap">
      <div className="dashboard" role="img" aria-label="RouteGate Admin UI current product preview">
        <aside className="dashboard-nav">
          <Brand compact />
          <div className="dashboard-menu">{nav.map((item, index) => <span className={index === 0 ? 'is-active' : ''} key={item}><i />{item}</span>)}</div>
          <div className="dashboard-user"><span>RG</span><div><strong>Admin</strong><small>RouteGate</small></div></div>
        </aside>
        <div className="dashboard-shell">
          <div className="dashboard-toolbar">
            <div className="dashboard-search">⌕ <span>{isEnglish ? 'Search' : 'Поиск'}</span><kbd>⌘ K</kbd></div>
            <div className="dashboard-tools"><span>?</span><span>{isEnglish ? 'EN' : 'RU'}</span><span>RG</span></div>
          </div>
          <div className="dashboard-main">
            <div className="dashboard-top"><div><span>{t.overview}</span><h3>{t.infrastructure}</h3></div><div className="health"><i />{t.healthy}</div></div>
            <div className="stats">{stats.map(([label, value, delta]) => <article key={label}><span>{label}</span><div><strong>{value}</strong><small>{delta}</small></div></article>)}</div>
            <WorldMap t={t} />
            <div className="dashboard-bottom">
              <div className="health-card">
                <div className="widget-heading"><strong>{isEnglish ? 'Runtime status' : 'Состояние runtime'}</strong><button type="button" tabIndex={-1} aria-hidden="true">•••</button></div>
                <div className="health-row"><span><i className="ok" />{isEnglish ? 'Manager / Agent' : 'Manager / Agent'}</span><b>{isEnglish ? 'Healthy' : 'Работают'}</b></div>
                <div className="health-row"><span><i className="ok" />VLESS / Reality</span><b>{isEnglish ? 'Ready' : 'Готово'}</b></div>
              </div>
              <div className="traffic-card"><span>{isEnglish ? 'Deployment' : 'Конфигурация'}</span><strong>{isEnglish ? 'Live' : 'Live'}</strong><div className="bars">{[5,7,6,9,8,10,9,12,11,13,12,14].map((height,index)=><i style={{height:`${height*2}px`}} key={index}/>)}</div></div>
            </div>
          </div>
        </div>
      </div>
      <span className="dashboard-caption">RouteGate Admin UI · Current product preview</span>
    </div>
  )
}

function CodePreview({ t }: { t: SiteContent['source'] }) {
  return <div className="code-window"><div className="code-toolbar"><div><i /><i /><i /></div><span>{t.repository}</span><small>Go</small></div><pre aria-label="RouteGate source code preview"><code>
    <span className="code-line"><em>type</em> ApplyRequest <em>struct</em> {'{'}</span><span className="code-line indent">ServerID <b>uuid.UUID</b></span><span className="code-line indent">Version  <b>int64</b></span><span className="code-line">{'}'}</span><span className="code-line empty"> </span><span className="code-line"><em>func</em> (s *Service) Apply(</span><span className="code-line indent">ctx <b>context.Context</b>,</span><span className="code-line indent">req <b>ApplyRequest</b>,</span><span className="code-line">) <b>error</b> {'{'}</span><span className="code-line comment indent">// render → validate → stage</span><span className="code-line indent"><em>if</em> err := s.validate(req); err != nil {'{'}</span><span className="code-line indent2"><em>return</em> err</span><span className="code-line indent">{'}'}</span><span className="code-line indent"><em>return</em> s.agent.Apply(ctx, req)</span><span className="code-line">{'}'}</span>
  </code></pre><div className="code-status"><span>main</span><span>open development</span><span>AGPLv3-or-later</span></div></div>
}

function AppHeader({ locale, setLocale, t }: { locale: Locale; setLocale: (value: Locale) => void; t: SiteContent }) {
  return <header className="header"><div className="container header-inner"><a href="#top" aria-label="RouteGate home"><Brand /></a><nav aria-label="Main navigation"><a href="#product">{t.nav.product}</a><a href="#open-source">{t.nav.openSource}</a><a href="#docs">{t.nav.docs}</a><a href="#roadmap">{t.nav.roadmap}</a><a href="#changelog">{t.nav.changelog}</a></nav><div className="header-actions"><a className="github-link" href={githubUrl} target="_blank" rel="noreferrer">GitHub <span>↗</span></a><button className="locale" type="button" onClick={() => setLocale(locale === 'ru' ? 'en' : 'ru')} aria-label={locale === 'ru' ? 'Switch to English' : 'Переключить на русский'}><span className={locale === 'ru' ? 'is-active' : ''}>RU</span><i>/</i><span className={locale === 'en' ? 'is-active' : ''}>EN</span></button><a className="button button--small button--primary" href="#start">{t.action.start}</a></div></div></header>
}

export function App() {
  const initialLocale = useMemo<Locale>(() => { const savedLocale = window.localStorage.getItem('routegate-locale'); if (savedLocale === 'ru' || savedLocale === 'en') return savedLocale; return navigator.language.toLowerCase().startsWith('ru') ? 'ru' : 'en' }, [])
  const [locale, setLocale] = useState<Locale>(initialLocale)
  const t = content[locale]
  const icons: Array<'server' | 'account' | 'route' | 'client'> = ['server', 'account', 'route', 'client']

  useEffect(() => {
    const descriptions: Record<Locale, string> = { en: 'RouteGate is an open-source self-hosted platform for managing Linux VPN servers, accounts, routing profiles, deployment, client access, and operational visibility.', ru: 'RouteGate — открытая self-hosted платформа управления Linux VPN-серверами, аккаунтами, маршрутными профилями, развёртыванием, клиентским доступом и состоянием инфраструктуры.' }
    document.documentElement.lang = locale
    document.title = locale === 'ru' ? 'RouteGate — управление Linux VPN-инфраструктурой' : 'RouteGate — Linux VPN Management Platform'
    document.querySelector<HTMLMetaElement>('meta[name="description"]')?.setAttribute('content', descriptions[locale])
    document.querySelector<HTMLMetaElement>('meta[property="og:description"]')?.setAttribute('content', descriptions[locale])
    document.querySelector<HTMLMetaElement>('meta[property="og:locale"]')?.setAttribute('content', locale === 'ru' ? 'ru_RU' : 'en_US')
    window.localStorage.setItem('routegate-locale', locale)
  }, [locale])

  return <div className="site" lang={locale}>
    <a className="skip-link" href="#main-content">{locale === 'ru' ? 'Перейти к содержимому' : 'Skip to content'}</a><AppHeader locale={locale} setLocale={setLocale} t={t} />
    <main id="main-content"><span id="top" className="anchor-target" aria-hidden="true" />
      <section className="hero"><div className="hero-grid container"><div className="hero-copy"><div className="eyebrow"><i />{t.hero.eyebrow}</div><h1>{t.hero.title}</h1><h2>{t.hero.subtitle}</h2><p>{t.hero.description}</p><p className="hero-note">{t.hero.note}</p><div className="hero-actions"><a className="button button--primary" href="#start">{t.action.start}<span>→</span></a><a className="button button--ghost" href={githubUrl} target="_blank" rel="noreferrer"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2a10 10 0 0 0-3.16 19.49c.5.09.68-.22.68-.48v-1.87c-2.78.6-3.37-1.18-3.37-1.18-.45-1.15-1.11-1.46-1.11-1.46-.91-.62.07-.61.07-.61 1 .07 1.53 1.03 1.53 1.03.9 1.53 2.34 1.09 2.91.83.09-.65.35-1.09.64-1.34-2.22-.25-4.55-1.11-4.55-4.94 0-1.09.39-1.98 1.03-2.68-.1-.25-.45-1.27.1-2.64 0 0 .84-.27 2.75 1.02A9.56 9.56 0 0 1 12 6.84a9.5 9.5 0 0 1 2.5.34c1.91-1.3 2.75-1.02 2.75-1.02.55 1.37.2 2.39.1 2.64.64.7 1.03 1.59 1.03 2.68 0 3.84-2.34 4.69-4.57 4.94.36.31.68.92.68 1.86V21c0 .27.18.58.69.48A10 10 0 0 0 12 2Z" /></svg>{t.action.github}</a></div><div className="hero-meta"><span>Linux</span><span>VLESS</span><span>Reality</span><span>Self-hosted</span></div></div><DashboardPreview t={t.dashboard} /></div></section>
      <section className="section product-section container" id="product"><div className="section-heading"><div><span>{t.product.eyebrow}</span><h2>{t.product.title}</h2></div><p>{t.product.intro}</p></div><div className="feature-grid">{t.product.cards.map((card,index)=><article className="feature-card" key={card.title}><span className="feature-icon"><Icon name={icons[index]} /></span><div><h3>{card.title}</h3><p>{card.text}</p></div></article>)}</div></section>
      <section className="section workflow-section" id="workflow"><div className="container"><div className="center-heading"><span>{t.workflow.eyebrow}</span><h2>{t.workflow.title}</h2></div><div className="workflow">{t.workflow.steps.map((step,index)=><article key={step.title}><div className="workflow-icon"><b>{index+1}</b><Icon name={icons[index]} /></div><div><h3>{step.title}</h3><p>{step.text}</p></div>{index<t.workflow.steps.length-1&&<i className="workflow-arrow" aria-hidden="true">→</i>}</article>)}</div></div></section>
      <section className="section source-section" id="open-source"><div className="container source-grid"><div className="source-copy"><span className="section-label">{t.source.eyebrow}</span><h2>{t.source.title}</h2><p>{t.source.text}</p><ul>{t.source.points.map(point=><li key={point}><i>✓</i>{point}</li>)}</ul><a href={githubUrl} target="_blank" rel="noreferrer">{t.action.github}<span>↗</span></a></div><CodePreview t={t.source} /></div></section>
      <section className="section deployment-section container" id="roadmap"><div className="section-heading"><div><span>{t.deployment.eyebrow}</span><h2>{t.deployment.title}</h2></div><p>{t.deployment.text}</p></div><div className="deployment-grid">{t.deployment.cards.map(card=><article key={card.title}><h3>{card.title}</h3><p>{card.text}</p></article>)}</div></section>
      <section className="final-cta container" id="start"><div className="cta-mark"><img src={assetUrl('routegate-symbol.svg')} alt="" /></div><div><h2>{t.cta.title}</h2><p>{t.cta.text}</p></div><div><a className="button button--light" href={docsUrl}>{t.action.start}<span>→</span></a><a className="button button--outline" href={githubUrl} target="_blank" rel="noreferrer">{t.action.github}</a></div></section>
    </main>
    <footer className="footer"><div className="container footer-grid"><div><Brand /><p>{t.footer.description}</p><small>© 2026 RouteGate</small></div><div><strong>{t.footer.project}</strong><a href="#product">{t.footer.items[0]}</a><a href="#roadmap">{t.footer.items[1]}</a></div><div><strong>{t.footer.resources}</strong><a id="docs" href={docsUrl}>{t.footer.items[2]}</a><a href={githubUrl} target="_blank" rel="noreferrer">{t.footer.items[3]}</a></div><div><strong>{t.footer.legal}</strong><a id="changelog" href={releasesUrl}>{t.footer.items[4]}</a><a href={licenseUrl}>{t.footer.items[5]}</a></div></div></footer>
  </div>
}
