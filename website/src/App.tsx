export function App() {
  return (
    <main
      style={{
        minHeight: '100vh',
        display: 'grid',
        placeItems: 'center',
        padding: '24px',
        background: '#0b0d10',
        color: '#f5f7fa',
        fontFamily: 'Inter, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
        textAlign: 'center',
      }}
    >
      <section style={{ maxWidth: '640px' }}>
        <div style={{ fontSize: '14px', letterSpacing: '0.12em', textTransform: 'uppercase', opacity: 0.6 }}>
          RouteGate
        </div>
        <h1 style={{ margin: '18px 0 12px', fontSize: 'clamp(36px, 7vw, 64px)', lineHeight: 1.05 }}>
          Site under maintenance
        </h1>
        <p style={{ margin: 0, fontSize: '18px', lineHeight: 1.6, opacity: 0.72 }}>
          We’re preparing the next version of the RouteGate website. Please check back soon.
        </p>
      </section>
    </main>
  )
}
