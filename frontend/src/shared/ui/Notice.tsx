import type { ReactNode } from 'react';

type NoticeProps = {
  children: ReactNode;
  tone?: 'info' | 'success' | 'warning' | 'error';
  announcement?: 'off' | 'polite' | 'assertive';
  className?: string;
  id?: string;
};

// Color describes meaning, not urgency. Static guidance must not become a live
// announcement merely because it uses warning/error styling.
export function Notice({ children, tone = 'info', announcement = 'off', className, id }: NoticeProps) {
  return (
    <div
      id={id}
      className={['form-message', tone !== 'info' && `form-message-${tone}`, className].filter(Boolean).join(' ')}
      role={announcement === 'assertive' ? 'alert' : announcement === 'polite' ? 'status' : undefined}
      aria-atomic={announcement !== 'off' ? true : undefined}
    >
      {children}
    </div>
  );
}
