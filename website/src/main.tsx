import React from 'react'
import ReactDOM from 'react-dom/client'
import { App } from './App'
import './styles.css'
import './hero-modern.css'
import './site-refresh.css'

const root = document.getElementById('root')!
const initialLocale = window.location.pathname === '/ru/' || window.location.pathname.startsWith('/ru/')
  ? 'ru'
  : 'en'

// The production build leaves crawlable locale-specific fallback content in
// the HTML. React owns the interactive tree once JavaScript starts.
root.replaceChildren()

ReactDOM.createRoot(root).render(
  <React.StrictMode>
    <App initialLocale={initialLocale} />
  </React.StrictMode>,
)
