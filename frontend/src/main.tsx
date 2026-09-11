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
    const small = document.querySelector('.vpn-account-edit-grid .field small');
    const page = document.querySelector('.vpn-accounts-page');
    const gridStyle = grid ? window.getComputedStyle(grid) : null;
    const rect = (el: Element | null) => (el ? JSON.stringify(el.getBoundingClientRect()) : 'n/a');
    overlay.textContent = [
      `UA: ${navigator.userAgent}`,
      `devicePixelRatio: ${window.devicePixelRatio}`,
      `window.innerWidth/innerHeight: ${window.innerWidth}/${window.innerHeight}`,
      `documentElement.clientWidth: ${document.documentElement.clientWidth}`,
      `visualViewport: ${window.visualViewport ? `width=${window.visualViewport.width} scale=${window.visualViewport.scale}` : 'n/a'}`,
      `body.scrollWidth: ${document.body.scrollWidth}`,
      `.vpn-accounts-page rect: ${rect(page)}`,
      `.vpn-account-edit-grid grid-template-columns: ${gridStyle ? gridStyle.gridTemplateColumns : 'n/a'}`,
      `.vpn-account-edit-grid rect: ${rect(grid)}`,
      `.field small rect: ${rect(small)}`,
    ].join('\n');
  }

  renderDebugOverlay();
  window.addEventListener('resize', renderDebugOverlay);
  setInterval(renderDebugOverlay, 1000);
}
