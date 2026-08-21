import { useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';

/**
 * Keep historical internal links usable while RouteGate converges on the
 * current navigation model. Config deployment is server-scoped and lives on
 * Server Details, so the old global /config-deploy entry point now guides the
 * administrator to choose the affected server instead of falling through to
 * the dashboard wildcard route.
 */
export function LegacyRouteRedirects() {
  const location = useLocation();
  const navigate = useNavigate();

  useEffect(() => {
    if (location.pathname === '/config-deploy') {
      navigate('/servers', { replace: true });
    }
  }, [location.pathname, navigate]);

  return null;
}
