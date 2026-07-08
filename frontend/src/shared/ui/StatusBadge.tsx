import { translateStatus } from '../i18n/i18n';

function toStatusClassName(status?: string | null): string {
  const normalizedStatus = status && status.trim() !== '' ? status : 'unknown';

  return normalizedStatus.toLowerCase().replace(/[^a-z0-9-]/g, '-');
}

type StatusBadgeProps = {
  status?: string | null;
  label?: string;
};

export function StatusBadge({ status, label }: StatusBadgeProps) {
  const normalizedStatus = status && status.trim() !== '' ? status : 'unknown';
  const statusClassName = toStatusClassName(normalizedStatus);

  return (
    <span className={`badge badge-${statusClassName}`} style={{ whiteSpace: 'nowrap' }}>
      {label ?? translateStatus(normalizedStatus)}
    </span>
  );
}
