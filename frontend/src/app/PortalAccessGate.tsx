import { type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Navigate, useLocation } from 'react-router-dom';
import { getPortalMe } from '../entities/portal/api/portalApi';
import { ApiError, clearAuthToken, getAuthToken } from '../shared/api/client';
import { t } from '../shared/i18n/i18n';
import './PortalAccessGate.css';

interface PortalAccessGateProps {
  children: ReactNode;
}

function portalReturnTo(pathname: string, search: string): string {
  return `${pathname}${search}`;
}

function loginTarget(returnTo: string): string {
  return `/login?returnTo=${encodeURIComponent(returnTo)}`;
}

export function PortalAccessGate({ children }: PortalAccessGateProps) {
  const location = useLocation();
  const isPortalRoute = location.pathname.startsWith('/portal');
  const authToken = getAuthToken();
  const returnTo = portalReturnTo(location.pathname, location.search);

  const portalMeQuery = useQuery({
    queryKey: ['portal-me'],
    queryFn: async () => {
      try {
        return await getPortalMe();
      } catch (error) {
        if (error instanceof ApiError && error.status === 401) {
          clearAuthToken();
        }
        throw error;
      }
    },
    enabled: isPortalRoute && Boolean(authToken),
    retry: false,
  });

  if (!isPortalRoute) {
    return children;
  }

  if (!authToken) {
    return <Navigate to={loginTarget(returnTo)} replace />;
  }

  if (portalMeQuery.isPending) {
    return (
      <div className="portal-access-gate-shell">
        <section className="portal-access-gate-card" aria-live="polite">
          <div className="portal-access-gate-eyebrow">RouteGate</div>
          <h1>{t('portalV2.checkingAccess')}</h1>
          <p>{t('portalV2.checkingAccessDescription')}</p>
        </section>
      </div>
    );
  }

  if (portalMeQuery.isError) {
    const error = portalMeQuery.error;

    if (error instanceof ApiError && error.status === 401) {
      return <Navigate to={loginTarget(returnTo)} replace />;
    }

    if (error instanceof ApiError && error.status === 403) {
      const signInAgain = () => {
        clearAuthToken();
        window.location.assign(loginTarget(returnTo));
      };

      return (
        <div className="portal-access-gate-shell">
          <section className="portal-access-gate-card portal-access-gate-card-warning">
            <div className="portal-access-gate-eyebrow">{t('app.portalSubtitle')}</div>
            <h1>{t('portalV2.accessDeniedTitle')}</h1>
            <p>{t('portalV2.accessDeniedDescription')}</p>
            <button className="primary-button" type="button" onClick={signInAgain}>
              {t('portalV2.signInAnotherAccount')}
            </button>
          </section>
        </div>
      );
    }

    return (
      <div className="portal-access-gate-shell">
        <section className="portal-access-gate-card portal-access-gate-card-warning">
          <div className="portal-access-gate-eyebrow">{t('app.portalSubtitle')}</div>
          <h1>{t('portalV2.portalUnavailableTitle')}</h1>
          <p>{t('portalV2.portalUnavailableDescription')}</p>
          <button className="primary-button" type="button" onClick={() => portalMeQuery.refetch()}>
            {t('portalV2.retry')}
          </button>
        </section>
      </div>
    );
  }

  return children;
}