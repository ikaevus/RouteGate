import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';

export function ScrollToTop() {
  const location = useLocation();

  useEffect(() => {
    const workspace = document.querySelector<HTMLElement>('[data-route-scroll-target]');
    if (workspace) {
      workspace.scrollIntoView({ block: 'start' });
      if (!workspace.contains(document.activeElement)) {
        workspace.focus({ preventScroll: true });
      }
      return;
    }
    window.scrollTo(0, 0);
  }, [location.pathname]);

  return null;
}
