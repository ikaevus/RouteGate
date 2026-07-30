import { useMemo, useState } from 'react'
import { content, type Locale, type SiteContent } from './content'

const githubUrl = 'https://github.com/ikaevus/RouteGate'

function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <span className={`brand ${compact ? 'brand--compact' : ''}`}>
      <img src="/routegate-symbol.svg" alt="" />
      <span>RouteGate</span>
    </span>
  )
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
  const regions = [
    { code: 'NA', count: 28, left: '20%', top: '39%' },
    { code: 'EU', count: 63, left: '49%', top: '31%' },
    { code: 'AS', count: 42, left: '75%', top: '43%' },
    { code: 'SA', count: 15, left: '31%', top: '70%' },
    { code: 'AF', count: 8, left: '52%', top: '61%' },
  ]

  return (
    <div className="map-widget">
      <div className="widget-heading">
        <div><strong>{t.map}</strong><span>156 / 189 online</span></div>
        <button aria-label="More map options">•••</button>
      </div>
      <div className="world-map">
        <svg viewBox="0 0 1000 430" role="img" aria-label={t.map}>
          <g className="map-grid">
            <path d="M0 86H1000M0 172H1000M0 258H1000M0 344H1000M125 0V430M250 0V430M375 0V430M500 0V430M625 0V430M750 0V430M875 0V430" />
          </g>
          <g className="map-land">
            <path d="M55 93l31-39 58-28 65 7 36 22 42 8 29 35-6 28-24 11-20 30-36 6-17 29-28 4-20 38-24 1-11-31-32-14-9-31-30-12-15-36 11-28z" />
            <path d="M177 255l31 3 29 21 20 37-7 44-25 50-18-10-10-43-22-27-10-38z" />
            <path d="M318 37l38-24 48 6 18 25-21 22-48 2-31-12z" />
            <path d="M424 86l35-27 46-5 36 19 12 23-28 11-19 22-45-4-21 18-27-13-12-27z" />
            <path d="M463 145l40-14 55 14 31 35 1 48-21 52-12 51-29 38-19-23-7-50-28-41-12-48-25-33z" />
            <path d="M536 74l56-33 80-17 79 17 58-4 61 22 70 13 42 33-16 28-46 8-34 28-46-5-31 24-50-5-36 25-45-18-43 10-32-27-43-5-12-28-33-17z" />
            <path d="M657 176l40-13 43 18 19 32-20 27-37-4-16-27-32-8z" />
            <path d="M778 292l55-28 58 9 50 35 9 40-37 34-67 11-58-24-25-38z" />
            <path d="M898 198l25-15 31 12 2 25-24 20-28-7z" />
            <path d="M954 358l17-6 14 11-8 13-18-2z" />
          </g>
          <g className="map-borders">
            <path d="M86 54l32 55 54 16 35 48M463 64l19 61 40 7M593 43l32 75 75 45M809 38l-16 107 47 19M833 264l15 64 65 54" />
          </g>
        </svg>
        {regions.map(region => (
          <span className="region-marker" style={{ left: region.left, top: region.top }} key={region.code}>
            <strong>{region.count}</strong><em>{region.code}</em>
          </span>
        ))}
      </div>
      <div className="map-legend">
        {regions.map(region => <span key={region.code}><i />{region.code}<b>{region.count}</b></span>)}
      </div>
    </div>
  )
}

function DashboardPreview({ t }: { t: SiteContent['dashboard'] }) {
  const isEnglish = t.overview === 'Overview'
  const nav = [
    t.overview,
    t.servers,
    t.accounts,
    isEnglish ? 'Configuration / Deploy' : 'Конфигурация / Deploy',
    isEnglish ? 'Routing profiles' : 'Маршрутные профили',
    'User portal',
  ]

  return (
    <div className="dashboard-wrap">
      <div className="dashboard" aria-label="RouteGate Admin UI preview">
        <aside className="dashboard-nav">
          <Brand compact />
          <div className="dashboard-menu">
            {nav.map((item, index) => <span className={index === 0 ? 'is-active' : ''} key={item}><i />{item}</span>)}
          </div>
          <div className="dashboard-user"><span>IK</span><div><strong>Admin</strong><small>admin@routegate</small></div></div>
        </aside>
        <div className="dashboard-shell">
          <div className="dashboard-toolbar">
            <div className="dashboard-search">⌕ <span>{isEnglish ? 'Search' : 'Поиск'}</span><kbd>⌘ K</kbd></div>
            <div className="dashboard-tools"><span>?</span><span>{isEnglish ? 'EN' : 'RU'}</span><span>IK</span></div>
          </div>
          <div className="dashboard-main">
            <div className="dashboard-top">
              <div><span>{t.overview}</span><h3>{t.infrastructure}</h3></div>
              <div className="health"><i />{t.healthy}</div>
            </div>
            <div className="stats">
              {[
                [isEnglish ? 'Active servers' : 'Активные серверы', '24 / 28', '86%'],
                [isEnglish ? 'Online agents' : 'Агенты онлайн', '156 / 189', '83%'],
                [isEnglish ? 'Active VPN users' : 'Активные VPN-пользователи', '842', '+5.2%'],
                [isEnglish ? 'Monthly traffic' : 'Трафик за месяц', '12.4 TB', '30d'],
              ].map(([label, value, delta]) => (
                <article key={label}><span>{label}</span><div><strong>{value}</strong><small>{delta}</small></div></article>
              ))}
            </div>
            <WorldMap t={t} />
            <div className="dashboard-bottom">
              <div className="health-card">
                <div className="widget-heading"><strong>{isEnglish ? 'Infrastructure health' : 'Состояние инфраструктуры'}</strong><button>•••</button></div>
                <div className="health-row"><span><i className="ok" />{isEnglish ? 'Healthy' : 'Работают'}</span><b>24</b></div>
                <div className="health-row"><span><i className="warn" />{isEnglish ? 'Attention' : 'Требуют внимания'}</span><b>4</b></div>
              </div>
              <div className="traffic-card">
                <span>{t.traffic}</span><strong>12.4 <small>TB</small></strong>
                <div className="bars">{[4, 6, 5, 9, 7, 12, 8, 11, 14, 12, 16, 13].map((height, index) => <i style={{ height: `${height * 2}px` }} key={index} />)}</div>
              </div>
            </div>
          </div>
        </div>
      </div>
      <span className="dashboard-caption">RouteGate Admin UI · Preview</span>
    </div>
  )
}

function CodePreview({ t }: { t: SiteContent['source'] }) {
  return (
    <div className="code-window">
      <div className="code-toolbar"><div><i /><i /><i /></div><span>{t.repository}</span><small>Go</small></div>
      <pre aria-label="RouteGate source code preview"><code>
        <span className="code-line"><em>type</em> ApplyRequest <em>struct</em> {'{'}</span>
        <span className="code-line indent">ServerID <b>uuid.UUID</b></span>
        <span className="code-line indent">Version  <b>int64</b></span>
        <span className="code-line">{'}'}</span>
        <span className="code-line empty"> </span>
        <span className="code-line"><em>func</em> (s *Service) Apply(</span>
        <span className="code-line indent">ctx <b>context.Context</b>,</span>
        <span className="code-line indent">req <b>ApplyRequest</b>,</span>
        <span className="code-line">) <b>error</b> {'{'}</span>
        <span className="code-line comment indent">// render → validate → stage</span>
        <span className="code-line indent"><em>if</em> err := s.validate(req); err != nil {'{'}</span>
        <span className="code-line indent2"><em>return</em> err</span>
        <span className="code-line indent">{'}'}</span>
        <span className="code-line indent"><em>return</em> s.agent.Apply(ctx, req)</span>
        <span className="code-line">{'}'}</span>
      </code></pre>
      <div className="code-status"><span>main</span><span>✓ tests passing</span><span>AGPLv3</span></div>
    </div>
  )
}

function AppHeader({ locale, setLocale, t }: { locale: Locale; setLocale: (value: Locale) => void; t: SiteContent }) {
  return (
    <header className="header">
      <div className="container header-inner">
        <a href="#top" aria-label="RouteGate home"><Brand /></a>
        <nav aria-label="Main navigation">
          <a href="#product">{t.nav.product}</a><a href="#open-source">{t.nav.openSource}</a>
          <a href="#docs">{t.nav.docs}</a><a href="#roadmap">{t.nav.roadmap}</a><a href="#changelog">{t.nav.changelog}</a>
        </nav>
        <div className="header-actions">
          <a className="github-link" href={githubUrl}>GitHub <span>↗</span></a>
          <button className="locale" onClick={() => setLocale(locale === 'ru' ? 'en' : 'ru')} aria-label="Switch language">{locale.toUpperCase()} <span>⌄</span></button>
          <a className="button button--small button--primary" href="#start">{t.action.start}</a>
        </div>
      </div>
    </header>
  )
}

export function App() {
  const initialLocale = useMemo<Locale>(() => navigator.language.toLowerCase().startsWith('ru') ? 'ru' : 'en', [])
  const [locale, setLocale] = useState<Locale>(initialLocale)
  const t = content[locale]
  const icons: Array<'server' | 'account' | 'route' | 'client'> = ['server', 'account', 'route', 'client']

  return (
    <div className="site" lang={locale}>
      <AppHeader locale={locale} setLocale={setLocale} t={t} />
      <main id="top">
        <section className="hero">
          <div className="hero-grid container">
            <div className="hero-copy">
              <div className="eyebrow"><i />{t.hero.eyebrow}</div>
              <h1>{t.hero.title}</h1>
              <h2>{t.hero.subtitle}</h2>
              <p>{t.hero.description}</p>
              <p className="hero-note">{t.hero.note}</p>
              <div className="hero-actions">
                <a className="button button--primary" href="#start">{t.action.start}<span>→</span></a>
                <a className="button button--ghost" href={githubUrl}>
                  <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2a10 10 0 0 0-3.16 19.49c.5.09.68-.22.68-.48v-1.87c-2.78.6-3.37-1.18-3.37-1.18-.45-1.15-1.11-1.46-1.11-1.46-.91-.62.07-.61.07-.61 1 .07 1.53 1.03 1.53 1.03.9 1.53 2.34 1.09 2.91.83.09-.65.35-1.09.64-1.34-2.22-.25-4.55-1.11-4.55-4.94 0-1.09.39-1.98 1.03-2.68-.1-.25-.45-1.27.1-2.64 0 0 .84-.27 2.75 1.02A9.56 9.56 0 0 1 12 6.84a9.5 9.5 0 0 1 2.5.34c1.91-1.3 2.75-1.02 2.75-1.02.55 1.37.2 2.39.1 2.64.64.7 1.03 1.59 1.03 2.68 0 3.84-2.34 4.69-4.57 4.94.36.31.68.92.68 1.86V21c0 .27.18.58.69.48A10 10 0 0 0 12 2Z" /></svg>
                  {t.action.github}
                </a>
              </div>
              <div className="hero-meta"><span>Linux</span><span>VLESS</span><span>Reality</span><span>Self-hosted</span></div>
            </div>
            <DashboardPreview t={t.dashboard} />
          </div>
        </section>

        <section className="section product-section container" id="product">
          <div className="section-heading"><div><span>{t.product.eyebrow}</span><h2>{t.product.title}</h2></div><p>{t.product.intro}</p></div>
          <div className="feature-grid">
            {t.product.cards.map((card, index) => (
              <article className="feature-card" key={card.title}>
                <span className="feature-icon"><Icon name={icons[index]} /></span>
                <div><h3>{card.title}</h3><p>{card.text}</p></div><b>0{index + 1}</b>
              </article>
            ))}
          </div>
        </section>

        <section className="section workflow-section" id="workflow">
          <div className="container">
            <div className="center-heading"><span>{t.workflow.eyebrow}</span><h2>{t.workflow.title}</h2></div>
            <div className="workflow">
              {t.workflow.steps.map((step, index) => (
                <article key={step.title}>
                  <div className="workflow-icon"><b>{index + 1}</b><Icon name={icons[index]} /></div>
                  <div><span>0{index + 1}</span><h3>{step.title}</h3><p>{step.text}</p></div>
                  {index < t.workflow.steps.length - 1 && <i className="workflow-arrow" aria-hidden="true">→</i>}
                </article>
              ))}
            </div>
          </div>
        </section>

        <section className="section source-section" id="open-source">
          <div className="container source-grid">
            <div className="source-copy">
              <span className="section-label">{t.source.eyebrow}</span><h2>{t.source.title}</h2><p>{t.source.text}</p>
              <ul>{t.source.points.map(point => <li key={point}><i>✓</i>{point}</li>)}</ul>
              <a href={githubUrl}>{t.action.github}<span>↗</span></a>
            </div>
            <CodePreview t={t.source} />
          </div>
        </section>

        <section className="section deployment-section container" id="roadmap">
          <div className="section-heading"><div><span>{t.deployment.eyebrow}</span><h2>{t.deployment.title}</h2></div><p>{t.deployment.text}</p></div>
          <div className="deployment-grid">
            {t.deployment.cards.map((card, index) => <article key={card.title}><span>0{index + 1}</span><h3>{card.title}</h3><p>{card.text}</p></article>)}
          </div>
        </section>

        <section className="final-cta container" id="start">
          <div className="cta-mark"><img src="/routegate-symbol.svg" alt="" /></div>
          <div><h2>{t.cta.title}</h2><p>{t.cta.text}</p></div>
          <div><a className="button button--light" href="#docs">{t.action.start}<span>→</span></a><a className="button button--outline" href={githubUrl}>{t.action.github}</a></div>
        </section>
      </main>

      <footer className="footer">
        <div className="container footer-grid">
          <div><Brand /><p>{t.footer.description}</p><small>© 2026 RouteGate</small></div>
          <div><strong>{t.footer.project}</strong><a href="#product">{t.footer.items[0]}</a><a href="#roadmap">{t.footer.items[1]}</a></div>
          <div><strong>{t.footer.resources}</strong><a id="docs" href={githubUrl}>{t.footer.items[2]}</a><a href={githubUrl}>{t.footer.items[3]}</a></div>
          <div><strong>{t.footer.legal}</strong><a id="changelog" href={githubUrl}>{t.footer.items[4]}</a><a href={githubUrl}>{t.footer.items[5]}</a></div>
        </div>
      </footer>
    </div>
  )
}

