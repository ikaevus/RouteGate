import React from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import { App } from './app/App';
import { GlobalSearchController } from './app/GlobalSearchController';
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
          <App />
          <GlobalSearchController />
        </BrowserRouter>
      </QueryClientProvider>
    </React.StrictMode>,
  );
}
