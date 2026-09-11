import React from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import { App } from './app/App';
import { BrandHomeNavigation } from './app/BrandHomeNavigation';
import { GlobalSearchController } from './app/GlobalSearchController';
import { LegacyRouteRedirects } from './app/LegacyRouteRedirects';
import { PortalAccessGate } from './app/PortalAccessGate';
import { ScrollToTop } from './app/ScrollToTop';
import './shared/styles.css';
import './shared/rg45.css';
import './shared/rg61.css';
import './shared/rg82.css';
import './shared/rg80.css';
import './shared/rg80-shell.css';
import './shared/rg80-dashboard.css';
import './shared/rg80-dashboard-tables.css';
import './shared/rg80-feature-reference.css';
import './shared/rg80-auth.css';
import './shared/rg80-locale.css';
import './shared/rg-spacing-audit.css';
import './shared/rg101-security.css';
import './shared/rg80-light.css';
import './shared/rg80-light-canvas-trial.css';
import './shared/rg80-light-polish.css';
import './shared/rg-shell-cleanup.css';
import './shared/rg-status-glass.css';
import './shared/rg130-mobile-safe-area.css';
import './shared/rg131-portal-mobile.css';
import './shared/rg114-ui-acceptance.css';

const storedTheme = window.localStorage.getItem('routegate.admin.theme');
document.documentElement.dataset.theme = storedTheme === 'light' ? 'light' : 'dark';

const queryClient = new QueryClient();
const rootElement = document.getElementById('root');

if (rootElement) {
  ReactDOM.createRoot(rootElement).render(
    <React.StrictMode>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <ScrollToTop />
          <LegacyRouteRedirects />
          <PortalAccessGate>
            <App />
            <BrandHomeNavigation />
            <GlobalSearchController />
          </PortalAccessGate>
        </BrowserRouter>
      </QueryClientProvider>
    </React.StrictMode>,
  );
}

if (new URLSearchParams(window.location.search).get('debug') === '1') {
  const overlay = document.createElement('div');
  overlay.style.cssText = 'position:fixed;top:0;left:0;right:0;z-index:2147483647;background:#000;color:#4ade80;font:10px/1.4 monospace;padding:6px 8px;white-space:pre-wrap;max-height:45vh;overflow:auto;pointer-events:auto;';
  document.body.appendChild(overlay);

  function renderDebugOverlay() {
    const grid = document.querySelector('.vpn-account-edit-grid');
    const panel = grid?.closest('.panel') ?? null;
    const input = document.querySelector('.vpn-account-edit-grid .field input');
    const width = (el: Element | null) => (el ? Math.round(el.getBoundingClientRect().width) : NaN);

    const gridWidth = width(grid);
    const inputWidth = width(input);
    const panelInner = panel
      ? panel.clientWidth
        - parseFloat(window.getComputedStyle(panel).paddingLeft)
        - parseFloat(window.getComputedStyle(panel).paddingRight)
      : NaN;
    const overflow = Number.isNaN(gridWidth) || Number.isNaN(panelInner)
      ? 'form not on screen'
      : gridWidth > panelInner + 1
        ? `YES, by ${Math.round(gridWidth - panelInner)}px`
        : 'no';

    overlay.textContent = [
      `viewport: ${window.innerWidth}x${window.innerHeight} dpr=${window.devicePixelRatio} scale=${window.visualViewport?.scale ?? '?'}`,
      `body.scrollWidth: ${document.body.scrollWidth}`,
      `panel inner width: ${Math.round(panelInner)}`,
      `edit-grid width:   ${gridWidth}`,
      `input width:       ${inputWidth}`,
      `grid-template-columns: ${grid ? window.getComputedStyle(grid).gridTemplateColumns : 'n/a'}`,
      `>>> FORM OVERFLOWS PANEL: ${overflow}`,
      `UA: ${navigator.userAgent}`,
    ].join('\n');
  }

  renderDebugOverlay();
  window.addEventListener('resize', renderDebugOverlay);
  setInterval(renderDebugOverlay, 1000);
}
