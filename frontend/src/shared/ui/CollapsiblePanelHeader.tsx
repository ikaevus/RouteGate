import { type KeyboardEvent, type ReactNode } from 'react';

export function CollapsiblePanelHeaderTitles({
  title,
  subtitle,
  open,
  onToggle,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  open: boolean;
  onToggle: () => void;
}) {
  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onToggle();
    }
  }

  return (
    <div
      className="panel-header-titles"
      role="button"
      tabIndex={0}
      aria-expanded={open}
      onClick={onToggle}
      onKeyDown={handleKeyDown}
    >
      <svg
        className={`panel-collapse-chevron${open ? '' : ' is-collapsed'}`}
        width="14"
        height="14"
        viewBox="0 0 16 16"
        aria-hidden="true"
      >
        <path d="M4 6l4 4 4-4" stroke="currentColor" strokeWidth="1.6" fill="none" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      <div>
        <div className="panel-title">{title}</div>
        {subtitle && <p className="panel-subtitle">{subtitle}</p>}
      </div>
    </div>
  );
}
