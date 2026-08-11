import { useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { t } from '../shared/i18n/i18n';

/**
 * RouteGate brand is the global "home" affordance.
 * In the Admin UI and User Portal it returns to the Admin Dashboard.
 */
export function BrandHomeNavigation() {
  const location = useLocation();
  const navigate = useNavigate();

  useEffect(() => {
    let detachAdminBrand: (() => void) | null = null;
    let detachPortalBrand: (() => void) | null = null;

    const enhanceAdminBrand = () => {
      if (detachAdminBrand) {
        return;
      }

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

      detachAdminBrand = () => {
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
    };

    const enhancePortalBrand = () => {
      if (detachPortalBrand) {
        return;
      }

      const brand = document.querySelector<HTMLAnchorElement>('.portal-app-shell .portal-brand');
      if (!brand) {
        return;
      }

      const previousHref = brand.getAttribute('href');
      const previousAriaLabel = brand.getAttribute('aria-label');

      // Keep native open-in-new-tab / middle-click behavior pointed at Dashboard too.
      brand.setAttribute('href', '/');
      brand.setAttribute('aria-label', t('navigation.overview'));

      const handleClick = (event: MouseEvent) => {
        if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
          return;
        }

        event.preventDefault();
        event.stopPropagation();
        navigate('/');
      };

      brand.addEventListener('click', handleClick);

      detachPortalBrand = () => {
        brand.removeEventListener('click', handleClick);

        if (previousHref === null) brand.removeAttribute('href');
        else brand.setAttribute('href', previousHref);

        if (previousAriaLabel === null) brand.removeAttribute('aria-label');
        else brand.setAttribute('aria-label', previousAriaLabel);
      };
    };

    const enhanceBrands = () => {
      enhanceAdminBrand();
      enhancePortalBrand();
    };

    enhanceBrands();

    const observer = new MutationObserver(enhanceBrands);
    observer.observe(document.body, { childList: true, subtree: true });

    return () => {
      observer.disconnect();
      detachAdminBrand?.();
      detachPortalBrand?.();
    };
  }, [location.pathname, navigate]);

  return null;
}
