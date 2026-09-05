import React from 'react'
import ReactDOM from 'react-dom/client'
import './maintenance.css'

const assetUrl = (path: string) => `${import.meta.env.BASE_URL}${path.replace(/^\//, '')}`

function MaintenancePage() {
  React.useEffect(() => {
    document.documentElement.lang = 'en'
    document.title = 'RouteGate — Maintenance'
    document.querySelector<HTMLMetaElement>('meta[name="description"]')?.setAttribute(
      'content',
      'RouteGate public website is temporarily unavailable while maintenance is in progress.',
    )
  }, [])

  return (
    <main className="maintenance-page">
      <section className="maintenance-card" aria-labelledby="maintenance-title">
        <div className="maintenance-brand">
          <img src={assetUrl('routegate-symbol.svg')} alt="" />
          <span>RouteGate</span>
        </div>
        <div className="maintenance-badge">Maintenance</div>
        <h1 id="maintenance-title">We’ll be back soon.</h1>
        <p>
          We are updating the RouteGate public website. The site is temporarily unavailable while maintenance is in progress.
        </p>
        <div className="maintenance-divider" aria-hidden="true" />
        <p className="maintenance-ru">
          Проводятся технические работы. Публичный сайт RouteGate временно недоступен.
        </p>
      </section>
    </main>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <MaintenancePage />
  </React.StrictMode>,
)
