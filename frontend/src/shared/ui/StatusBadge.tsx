import { translateStatus } from '../i18n/i18n';

function toStatusClassName(status?: string | null): string {
  const normalizedStatus = status && status.trim() !== '' ? status : 'unknown';

  return normalizedStatus.toLowerCase().replace(/[^a-z0-9-]/g, '-');
}

const calmPositiveStatuses = new Set([
  'online',
  'active',
  'validated',
  'applied',
  'commit-confirmed',
  'succeeded',
  'success',
  'compatible',
  'ready',
  'healthy',
]);

function sentenceCaseUpperLabel(value: string): string {
  const trimmed = value.trim();
  const casedCharacters = Array.from(trimmed).filter(
    (character) => character.toLocaleLowerCase() !== character.toLocaleUpperCase(),
  );

  if (casedCharacters.length < 2) {
    return trimmed;
  }

  const isAllUppercase = casedCharacters.every(
    (character) => character === character.toLocaleUpperCase(),
  );
  if (!isAllUppercase) {
    return trimmed;
  }

  const lower = trimmed.toLocaleLowerCase();
  return `${lower.charAt(0).toLocaleUpperCase()}${lower.slice(1)}`;
}

type StatusBadgeProps = {
  status?: string | null;
  label?: string;
};

export function StatusBadge({ status, label }: StatusBadgeProps) {
  const normalizedStatus = status && status.trim() !== '' ? status : 'unknown';
  const statusClassName = toStatusClassName(normalizedStatus);
  const translatedLabel = label ?? translateStatus(normalizedStatus);
  const displayLabel = calmPositiveStatuses.has(statusClassName)
    ? sentenceCaseUpperLabel(translatedLabel)
    : translatedLabel;

  return (
    <span className={`badge badge-${statusClassName}`} style={{ whiteSpace: 'nowrap' }}>
      {displayLabel}
    </span>
  );
}
