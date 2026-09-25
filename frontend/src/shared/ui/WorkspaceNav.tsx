import './WorkspaceNav.css';
import { useEffect, useRef } from 'react';
import { NavLink, useLocation } from 'react-router-dom';

export interface WorkspaceNavItem {
  href: string;
  label: string;
}

export function WorkspaceNav({ label, items }: { label: string; items: WorkspaceNavItem[] }) {
  const navRef = useRef<HTMLElement>(null);
  const { pathname } = useLocation();

  useEffect(() => {
    const nav = navRef.current;
    if (!nav) return;
    const revealActive = () => {
      const active = nav.querySelector<HTMLElement>('[aria-current="page"]');
      if (!active) return;
      const navBounds = nav.getBoundingClientRect();
      const activeBounds = active.getBoundingClientRect();
      // Only scroll the navigation strip; preserve the workspace's vertical position.
      if (activeBounds.left < navBounds.left || activeBounds.right > navBounds.right) {
        nav.scrollLeft += activeBounds.left - navBounds.left - (nav.clientWidth - activeBounds.width) / 2;
      }
    };
    revealActive();
    const observer = new ResizeObserver(revealActive);
    observer.observe(nav);
    return () => observer.disconnect();
  }, [pathname]);

  return (
    <nav ref={navRef} className="workspace-nav" aria-label={label}>
      {items.map((item) => (
        <NavLink
          key={item.href}
          to={item.href}
          className={({ isActive }) => `workspace-nav-link${isActive ? ' is-active' : ''}`}
        >
          {item.label}
        </NavLink>
      ))}
    </nav>
  );
}
