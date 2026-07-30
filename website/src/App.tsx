import { useMemo, useState } from 'react'
import { content, type Locale, type SiteContent } from './content'

const githubUrl = 'https://github.com/ikaevus/RouteGate'

function Brand({ compact = false }: { compact?: boolean }) {
  return <span className={`brand ${compact ? 'brand--compact' : ''}`}><img src="/routegate-symbol.svg" alt="" /><span>RouteGate</span></span>
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
  return <div className="map-widget">
    <div className="widget-heading"><div><strong>{t.map}</strong><span>{t.online}</span></div><button aria-label="More map options">•••</button></div>
    <div className="world-map">
      <svg viewBox="0 0 1000 430" role="img" aria-label={t.map}>
        <g className="map-grid"><path d="M0 108H1000M0 215H1000M0 322H1000M250 0V430M500 0V430M750 0V430" /></g>
        <g className="map-land">
          <path d="M73 73l67-39 86 7 53 34 13 45-28 28-25 3-10 34-31 25-10 37-33 21-25-34-24-30-40-21-23-47 15-35z" />
          <path d="M222 249l35 18 24 54-10 49-29 49-21-34-14-53-28-47z" />
          <path d="M428 72l41-20 51 9 24 21-17 20-46-2-19 24-36-8-19-19z" />
          <path d="M480 137l59-13 50 35 8 56-27 77-33 67-26-42-8-66-33-47-22-38z" />
          <path d="M528 79l72-37 103-7 66 22 76-6 95 42-26 38-58 5-34 32-60-13-50 20-60-30-52 11-39-31z" />
          <path d="M677 174l49-8 45 24-18 40-35 8-25-23z" /><path d="M790 295l64-22 73 25 20 42-42 47-81-3-50-37z" />
          <path d="M914 209l22-10 21 12-16 23-22-5z" /><path d="M321 38l29-25 44 5 5 28-31 21-43-7z" />
        </g>
        <g className="map-links"><path d="M226 156Q430 48 527 135" /><path d="M527 135Q687 68 780 171" /><path d="M527 135Q680 265 845 329" /></g>
      </svg>
      {[
        ['Frankfurt', '50.5%', '30%'], ['New York', '23%', '39%'], ['Singapore', '78%', '59%'],
        ['Tokyo', '86%', '38%'], ['São Paulo', '29%', '71%'],
      ].map(([label, left, top]) => <span className="map-pin" style={{ left, top }} key={label}><i /><em>{label}</em></span>)}
    </div>
  </div>
}

function DashboardPreview({ t }: { t: SiteContent['dashboard'] }) {
  const nav = [t.overview, t.servers, t.accounts, t.clients, t.traffic]
  return <div className="dashboard-wrap">
    <div className="dashboard-glow" />
    <div className="dashboard" aria-label="RouteGate Admin UI preview">
      <aside className="dashboard-nav"><Brand compact /><div className="dashboard-menu">
        {nav.map((item, index) => <span className={index === 0 ? 'is-active' : ''} key={item}><i />{item}</span>)}
      </div><div className="dashboard-user"><span>IK</span><div><strong>Admin</strong><small>admin@routegate</small></div></div></aside>
      <div className="dashboard-main">
        <div className="dashboard-top"><div><span>{t.overview}</span><h3>{t.infrastructure}</h3></div><div className="health"><i />{t.healthy}</div></div>
        <div className="stats">{[[t.servers,'5','+1'],[t.accounts,'48','+8'],[t.clients,'32','+5'],[t.traffic,'2.4 TB','30d']].map(([label,value,delta]) =>
          <article key={label}><span>{label}</span><div><strong>{value}</strong><small>{delta}</small></div></article>)}</div>
        <WorldMap t={t} />
        <div className="dashboard-bottom">
          <div className="activity-card"><div className="widget-heading"><strong>{t.activity}</strong><button>•••</button></div>
            <p><i className="ok" /><span><b>de-fra-01</b>{t.applied}</span><time>2m</time></p><p><i /><span><b>us-nyc-01</b>{t.connected}</span><time>8m</time></p></div>
          <div className="latency-card"><span>{t.latency}</span><strong>42 <small>ms</small></strong><div className="bars">{[4,6,5,9,7,12,8,11,14,12,16,13].map((h,i) => <i style={{height:`${h*2}px`}} key={i} />)}</div></div>
        </div>
      </div>
    </div><span className="dashboard-caption">RouteGate Admin UI · Preview</span>
  </div>
}

function CodePreview({ t }: { t: SiteContent['source'] }) {
  return <div className="code-window"><div className="code-toolbar"><div><i /><i /><i /></div><span>{t.repository}</span><small>Go</small></div>
    <pre aria-label="RouteGate source code preview"><code>
      <span className="code-line"><em>type</em> ApplyRequest <em>struct</em> {'{'}</span><span className="code-line indent">ServerID <b>uuid.UUID</b></span>
      <span className="code-line indent">Version  <b>int64</b></span><span className="code-line">{'}'}</span><span className="code-line empty"> </span>
      <span className="code-line"><em>func</em> (s *Service) Apply(</span><span className="code-line indent">ctx <b>context.Context</b>,</span>
      <span className="code-line indent">req <b>ApplyRequest</b>,</span><span className="code-line">) <b>error</b> {'{'}</span>
      <span className="code-line comment indent">// render → validate → stage</span><span className="code-line indent"><em>if</em> err := s.validate(req); err != nil {'{'}</span>
      <span className="code-line indent2"><em>return</em> err</span><span className="code-line indent">{'}'}</span><span className="code-line indent"><em>return</em> s.agent.Apply(ctx, req)</span><span className="code-line">{'}'}</span>
    </code></pre><div className="code-status"><span>main</span><span>✓ tests passing</span><span>AGPLv3</span></div></div>
}

function AppHeader({ locale, setLocale, t }: { locale: Locale; setLocale: (value: Locale) => void; t: SiteContent }) {
  return <header className="header"><div className="container header-inner"><a href="#top" aria-label="RouteGate home"><Brand /></a>
    <nav aria-label="Main navigation"><a href="#product">{t.nav.product}</a><a href="#open-source">{t.nav.openSource}</a><a href="#docs">{t.nav.docs}</a><a href="#roadmap">{t.nav.roadmap}</a><a href="#changelog">{t.nav.changelog}</a></nav>
    <div className="header-actions"><a className="github-link" href={githubUrl}>GitHub <span>↗</span></a><button className="locale" onClick={() => setLocale(locale === 'ru' ? 'en' : 'ru')} aria-label="Switch language">{locale.toUpperCase()} <span>⌄</span></button><a className="button button--small button--primary" href="#start">{t.action.start}</a></div>
  </div></header>
}

export function App() {
  const initialLocale = useMemo<Locale>(() => navigator.language.toLowerCase().startsWith('ru') ? 'ru' : 'en', [])
  const [locale, setLocale] = useState<Locale>(initialLocale)
  const t = content[locale]
  const icons: Array<'server' | 'account' | 'route' | 'client'> = ['server','account','route','client']
  return <div className="site" lang={locale}><AppHeader locale={locale} setLocale={setLocale} t={t} /><main id="top">
    <section className="hero"><div className="hero-grid container"><div className="hero-copy"><div className="eyebrow"><i />{t.hero.eyebrow}</div><h1>{t.hero.title}</h1><h2>{t.hero.subtitle}</h2><p>{t.hero.description}</p><p className="hero-note">{t.hero.note}</p>
      <div className="hero-actions"><a className="button button--primary" href="#start">{t.action.start}<span>→</span></a><a className="button button--ghost" href={githubUrl}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2a10 10 0 0 0-3.16 19.49c.5.09.68-.22.68-.48v-1.87c-2.78.6-3.37-1.18-3.37-1.18-.45-1.15-1.11-1.46-1.11-1.46-.91-.62.07-.61.07-.61 1 .07 1.53 1.03 1.53 1.03.9 1.53 2.34 1.09 2.91.83.09-.65.35-1.09.64-1.34-2.22-.25-4.55-1.11-4.55-4.94 0-1.09.39-1.98 1.03-2.68-.1-.25-.45-1.27.1-2.64 0 0 .84-.27 2.75 1.02A9.56 9.56 0 0 1 12 6.84a9.5 9.5 0 0 1 2.5.34c1.91-1.3 2.75-1.02 2.75-1.02.55 1.37.2 2.39.1 2.64.64.7 1.03 1.59 1.03 2.68 0 3.84-2.34 4.69-4.57 4.94.36.31.68.92.68 1.86V21c0 .27.18.58.69.48A10 10 0 0 0 12 2Z" /></svg>{t.action.github}</a></div>
      <div className="hero-meta"><span>Linux</span><span>VLESS</span><span>Reality</span><span>Self-hosted</span></div></div><DashboardPreview t={t.dashboard} /></div></section>
    <section className="section product-section container" id="product"><div className="section-heading"><div><span>{t.product.eyebrow}</span><h2>{t.product.title}</h2></div><p>{t.product.intro}</p></div><div className="feature-grid">{t.product.cards.map((card,index) => <article className="feature-card" key={card.title}><span className="feature-icon"><Icon name={icons[index]} /></span><div><h3>{card.title}</h3><p>{card.text}</p></div><b>0{index+1}</b></article>)}</div></section>
    <section className="section workflow-section"><div className="container"><div className="center-heading"><span>{t.workflow.eyebrow}</span><h2>{t.workflow.title}</h2></div><div className="workflow">{t.workflow.steps.map((step,index) => <article key={step.title}><b>{index+1}</b><div className="workflow-dot" /><h3>{step.title}</h3><p>{step.text}</p></article>)}</div></div></section>
    <section className="section source-section" id="open-source"><div className="container source-grid"><div className="source-copy"><span className="section-label">{t.source.eyebrow}</span><h2>{t.source.title}</h2><p>{t.source.text}</p><ul>{t.source.points.map(point => <li key={point}><i>✓</i>{point}</li>)}</ul><a href={githubUrl}>{t.action.github}<span>↗</span></a></div><CodePreview t={t.source} /></div></section>
    <section className="section deployment-section container" id="roadmap"><div className="section-heading"><div><span>{t.deployment.eyebrow}</span><h2>{t.deployment.title}</h2></div><p>{t.deployment.text}</p></div><div className="deployment-grid">{t.deployment.cards.map((card,index) => <article key={card.title}><span>0{index+1}</span><h3>{card.title}</h3><p>{card.text}</p></article>)}</div></section>
    <section className="final-cta container" id="start"><div className="cta-mark"><img src="/routegate-symbol.svg" alt="" /></div><div><h2>{t.cta.title}</h2><p>{t.cta.text}</p></div><div><a className="button button--light" href="#docs">{t.action.start}<span>→</span></a><a className="button button--outline" href={githubUrl}>{t.action.github}</a></div></section>
  </main><footer className="footer"><div className="container footer-grid"><div><Brand /><p>{t.footer.description}</p><small>© 2026 RouteGate</small></div><div><strong>{t.footer.project}</strong><a href="#product">{t.footer.items[0]}</a><a href="#roadmap">{t.footer.items[1]}</a></div><div><strong>{t.footer.resources}</strong><a id="docs" href={githubUrl}>{t.footer.items[2]}</a><a href={githubUrl}>{t.footer.items[3]}</a></div><div><strong>{t.footer.legal}</strong><a id="changelog" href={githubUrl}>{t.footer.items[4]}</a><a href={githubUrl}>{t.footer.items[5]}</a></div></div></footer></div>
}
