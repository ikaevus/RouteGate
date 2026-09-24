import { NavLink } from 'react-router-dom';

export interface WorkspaceNavItem {
  href: string;
  label: string;
}

export function WorkspaceNav({ label, items }: { label: string; items: WorkspaceNavItem[] }) {
  return (
    <nav className="workspace-nav" aria-label={label}>
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
