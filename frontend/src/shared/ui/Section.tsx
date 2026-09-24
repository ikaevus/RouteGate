import { useId, type ReactNode } from 'react';
import './Section.css';

interface SectionProps {
  title: string;
  description?: string;
  aside?: ReactNode;
  tone?: 'default' | 'danger';
  children: ReactNode;
}

export function Section({ title, description, aside, tone = 'default', children }: SectionProps) {
  const titleId = useId();

  return (
    <section className={`panel workspace-section${tone === 'danger' ? ' workspace-section-danger' : ''}`} aria-labelledby={titleId}>
      <div className="workspace-section-header">
        <div>
          <h3 id={titleId}>{title}</h3>
          {description && <p>{description}</p>}
        </div>
        {aside}
      </div>
      {children}
    </section>
  );
}
