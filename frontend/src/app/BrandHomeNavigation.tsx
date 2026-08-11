import { useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { t } from '../shared/i18n/i18n';

/**
 * Keeps the RouteGate brand behavior consistent across shells.
 * Portal/Auth already use real links; the Admin sidebar brand is legacy markup,
 * so upgrade it to an accessible Dashboard link without changing its layout.
 */
export function BrandHomeNavigation() {
  const location = useLocation();
  const navigate = useNavigate();

  useEffect(() => {
    const brand = document.querySelector<HTMLElement>('.routegate-admin-shell .routegate-brand');
    if (!brand) {
      return;
    }

    const previousRole = brand.getAttribute('role');
    const previousTabIndex = brand.getAttribute('tabindex');
    const previousAriaLabel = brand.getAttribute('aria-label');
    const previousCursor = brand.style.cursor;

    brand.setAttribute('role', 'link');
    brand.setAttribute('tabindex', '0');
    brand.setAttribute('aria-label', t('navigation.overview'));
    brand.style.cursor = 'pointer';

    const goHome = () => navigate('/');

    const handleClick = (event: MouseEvent) => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
        return;
      }
      goHome();
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Enter' && event.key !== ' ') {
        return;
      }
      event.preventDefault();
      goHome();
    };

    brand.addEventListener('click', handleClick);
    brand.addEventListener('keydown', handleKeyDown);

    return () => {
      brand.removeEventListener('click', handleClick);
      brand.removeEventListener('keydown', handleKeyDown);

      if (previousRole === null) brand.removeAttribute('role');
      else brand.setAttribute('role', previousRole);

      if (previousTabIndex === null) brand.removeAttribute('tabindex');
      else brand.setAttribute('tabindex', previousTabIndex);

      if (previousAriaLabel === null) brand.removeAttribute('aria-label');
      else brand.setAttribute('aria-label', previousAriaLabel);

      brand.style.cursor = previousCursor;
    };
  }, [location.pathname, navigate]);

  return null;
}
