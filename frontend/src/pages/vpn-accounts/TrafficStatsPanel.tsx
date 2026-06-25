import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  getVpnAccountTraffic,
  updateVpnAccountTrafficLimit,
  type UpdateTrafficLimitRequest,
} from '../../entities/vpnAccount/api/vpnAccountApi';

const BYTES_PER_GIB = 1024 ** 3;
const BPS_PER_MBIT = 1_000_000;

function formatBytes(value?: number | null): string {
  if (value === undefined || value === null || !Number.isFinite(value)) {
    return '-';
  }

  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let unitIndex = 0;
  let size = Math.max(0, value);

  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }

  const fractionDigits = size >= 10 || unitIndex === 0 ? 0 : 1;
  return `${size.toFixed(fractionDigits)} ${units[unitIndex]}`;
}

function formatDateTime(value?: string | null): string {
  if (!value) {
    return '-';
  }

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatNumberInput(value: number): string {
  if (!Number.isFinite(value)) {
    return '';
  }

  return Number.isInteger(value) ? String(value) : value.toFixed(2).replace(/\.?0+$/, '');
}

function formatPercent(value?: number | null): string {
  if (value === undefined || value === null || !Number.isFinite(value)) {
    return '-';
  }

  return `${value.toFixed(value >= 10 ? 0 : 1)}%`;
}

function formatStatus(value?: string | null): string {
  if (!value) {
    return 'not enforced';
  }

  return value.replace(/_/g, ' ');
}

function parseOptionalNumber(
  value: string,
  label: string,
  options: { allowZero: boolean } = { allowZero: false },
): number | null {
  const trimmed = value.trim();
  if (trimmed === '') {
    return null;
  }

  const parsed = Number(trimmed);
  const isBelowMinimum = options.allowZero ? parsed < 0 : parsed <= 0;
  if (!Number.isFinite(parsed) || isBelowMinimum) {
    throw new Error(
      `${label} must be a ${options.allowZero ? 'non-negative' : 'positive'} number or empty.`,
    );
  }

  return parsed;
}

function TrafficMetricCard({ label, value, meta }: { label: string; value: string; meta?: string }) {
  return (
    <div className="traffic-metric-card">
      <span className="traffic-metric-label">{label}</span>
      <strong className="traffic-metric-value">{value}</strong>
      {meta && <span className="traffic-metric-meta">{meta}</span>}
    </div>
  );
}

export function TrafficStatsPanel({ accountId }: { accountId: string }) {
  const queryClient = useQueryClient();
  const [monthlyLimitGiB, setMonthlyLimitGiB] = useState('');
  const [hardLimitEnabled, setHardLimitEnabled] = useState(false);
  const [speedLimitMbps, setSpeedLimitMbps] = useState('');
  const [resetDay, setResetDay] = useState('1');
  const [formError, setFormError] = useState<string | null>(null);

  const trafficQuery = useQuery({
    queryKey: ['vpn-account-traffic', accountId],
    queryFn: () => getVpnAccountTraffic(accountId),
    enabled: Boolean(accountId),
  });

  useEffect(() => {
    const limit = trafficQuery.data?.limit;

    setMonthlyLimitGiB(
      limit?.monthlyLimitBytes !== undefined && limit.monthlyLimitBytes !== null
        ? formatNumberInput(limit.monthlyLimitBytes / BYTES_PER_GIB)
        : '',
    );
    setHardLimitEnabled(limit?.hardLimitEnabled ?? false);
    setSpeedLimitMbps(
      limit?.speedLimitBps !== undefined && limit.speedLimitBps !== null
        ? formatNumberInput(limit.speedLimitBps / BPS_PER_MBIT)
        : '',
    );
    setResetDay(String(limit?.resetDay ?? 1));
    setFormError(null);
  }, [trafficQuery.data?.limit, accountId]);

  const updateLimitMutation = useMutation({
    mutationFn: (request: UpdateTrafficLimitRequest) => updateVpnAccountTrafficLimit(accountId, request),
    onMutate: () => {
      setFormError(null);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['vpn-account-traffic', accountId] });
    },
  });

  const traffic = trafficQuery.data;
  const limit = traffic?.limit;
  const monthlyLimitBytes = limit?.monthlyLimitBytes;
  const hasMonthlyLimit = monthlyLimitBytes !== undefined && monthlyLimitBytes !== null;
  const usedPercent = useMemo(() => {
    if (!traffic) {
      return null;
    }

    if (limit?.usedPercent !== undefined && limit.usedPercent !== null) {
      return limit.usedPercent;
    }

    if (monthlyLimitBytes !== undefined && monthlyLimitBytes !== null && monthlyLimitBytes > 0) {
      return (traffic.usage.totalBytes / monthlyLimitBytes) * 100;
    }

    return null;
  }, [limit?.usedPercent, monthlyLimitBytes, traffic]);

  const progressPercent = Math.min(100, Math.max(0, usedPercent ?? 0));
  const limitBadge = !hasMonthlyLimit
    ? { className: 'badge badge-pending', label: 'No limit' }
    : limit?.enforced
      ? { className: 'badge badge-failed', label: 'Over limit' }
      : limit?.limitReached
        ? { className: 'badge badge-failed', label: 'Limit reached' }
        : limit?.hardLimitEnabled
          ? { className: 'badge badge-in-progress', label: 'Hard limit' }
          : { className: 'badge badge-active', label: 'Soft limit' };
  const enforcementBadge = limit?.enforced
    ? { className: 'badge badge-failed', label: 'Enforced' }
    : limit?.enforcementStatus === 'within_limit'
      ? { className: 'badge badge-active', label: 'Within limit' }
      : { className: 'badge badge-pending', label: 'Not enforced' };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    try {
      const monthlyLimit = parseOptionalNumber(monthlyLimitGiB, 'Monthly limit', { allowZero: true });
      const speedLimit = parseOptionalNumber(speedLimitMbps, 'Speed limit');
      const parsedResetDay = Number.parseInt(resetDay, 10);

      if (!Number.isInteger(parsedResetDay) || parsedResetDay < 1 || parsedResetDay > 28) {
        throw new Error('Reset day must be between 1 and 28.');
      }

      updateLimitMutation.mutate({
        monthlyLimitBytes: monthlyLimit === null ? null : Math.round(monthlyLimit * BYTES_PER_GIB),
        hardLimitEnabled,
        speedLimitBps: speedLimit === null ? null : Math.round(speedLimit * BPS_PER_MBIT),
        resetDay: parsedResetDay,
      });
    } catch (error) {
      setFormError(error instanceof Error ? error.message : 'Traffic limit form is invalid.');
    }
  };

  return (
    <div className="panel traffic-panel">
      <div className="panel-header">
        <div>
          <div className="panel-title">Traffic usage and limits</div>
          <p className="panel-subtitle">
            Current billing-period usage summary, per-account traffic policy, and persisted enforcement state.
          </p>
        </div>
      </div>

      {trafficQuery.isLoading && <p className="empty-state">Loading traffic usage...</p>}

      {trafficQuery.isError && (
        <div className="form-message form-message-error">Failed to load traffic usage.</div>
      )}

      {traffic && (
        <div className="traffic-panel-content">
          <div className="traffic-summary-grid">
            <TrafficMetricCard
              label="Uploaded"
              value={formatBytes(traffic.usage.txBytes)}
              meta="TX traffic"
            />
            <TrafficMetricCard
              label="Downloaded"
              value={formatBytes(traffic.usage.rxBytes)}
              meta="RX traffic"
            />
            <TrafficMetricCard
              label="Total"
              value={formatBytes(traffic.usage.totalBytes)}
              meta={`${formatDateTime(traffic.period.from)} → ${formatDateTime(traffic.period.to)}`}
            />
          </div>

          <div className="traffic-limit-card">
            <div className="traffic-limit-header">
              <div>
                <div className="traffic-limit-title">Monthly limit</div>
                <p className="panel-subtitle">
                  {hasMonthlyLimit
                    ? `${formatBytes(traffic.usage.totalBytes)} used of ${formatBytes(monthlyLimitBytes)}`
                    : 'No monthly traffic limit is configured for this account.'}
                </p>
              </div>
              <span className={limitBadge.className}>{limitBadge.label}</span>
            </div>

            <div className="traffic-progress-track" aria-label="Traffic limit usage progress">
              <div className="traffic-progress-fill" style={{ width: `${progressPercent}%` }} />
            </div>

            <div className="traffic-limit-meta">
              <span>Used: {formatPercent(usedPercent)}</span>
              <span>Remaining: {formatBytes(limit?.remainingBytes)}</span>
              <span>Reset day: {limit?.resetDay ?? 1}</span>
              <span>Speed limit: {limit?.speedLimitBps ? `${formatNumberInput(limit.speedLimitBps / BPS_PER_MBIT)} Mbps` : 'not set'}</span>
              <span>Updated: {formatDateTime(limit?.updatedAt)}</span>
            </div>
          </div>

          <div className="traffic-limit-card">
            <div className="traffic-limit-header">
              <div>
                <div className="traffic-limit-title">Enforcement state</div>
                <p className="panel-subtitle">
                  RouteGate now persists over-limit state; runtime config exclusion will be connected in a later apply step.
                </p>
              </div>
              <span className={enforcementBadge.className}>{enforcementBadge.label}</span>
            </div>

            <div className="traffic-limit-meta">
              <span>Status: {formatStatus(limit?.enforcementStatus)}</span>
              <span>Exceeded at: {formatDateTime(limit?.limitExceededAt)}</span>
              <span>Evaluated: {formatDateTime(limit?.enforcementUpdatedAt)}</span>
            </div>
          </div>

          <form className="traffic-limit-form" onSubmit={handleSubmit}>
            <div>
              <div className="panel-title token-snippet-title">Limit settings</div>
              <p className="panel-subtitle">
                Empty monthly limit means unlimited traffic. Hard limit marks this account for persisted over-limit enforcement state.
              </p>
            </div>

            <div className="traffic-limit-form-grid">
              <label className="field">
                <span>Monthly limit, GiB</span>
                <input
                  min="0"
                  step="0.01"
                  type="number"
                  value={monthlyLimitGiB}
                  placeholder="Unlimited"
                  onChange={(event) => setMonthlyLimitGiB(event.target.value)}
                />
              </label>

              <label className="field">
                <span>Speed limit, Mbps</span>
                <input
                  min="0"
                  step="0.01"
                  type="number"
                  value={speedLimitMbps}
                  placeholder="Not set"
                  onChange={(event) => setSpeedLimitMbps(event.target.value)}
                />
              </label>

              <label className="field">
                <span>Reset day</span>
                <input
                  min="1"
                  max="28"
                  step="1"
                  type="number"
                  value={resetDay}
                  onChange={(event) => setResetDay(event.target.value)}
                />
              </label>

              <div className="traffic-checkbox-field">
                <label>
                  <input
                    type="checkbox"
                    checked={hardLimitEnabled}
                    onChange={(event) => setHardLimitEnabled(event.target.checked)}
                  />
                  <span>Hard limit</span>
                </label>
                <p>Persist over-limit state when reported usage reaches the configured limit.</p>
              </div>
            </div>

            {formError && <div className="form-message form-message-error">{formError}</div>}
            {updateLimitMutation.isError && (
              <div className="form-message form-message-error">Failed to save traffic limit.</div>
            )}
            {updateLimitMutation.isSuccess && !updateLimitMutation.isPending && (
              <div className="form-message">Traffic limit saved.</div>
            )}

            <div className="traffic-form-actions">
              <button
                className="primary-button"
                type="submit"
                disabled={updateLimitMutation.isPending}
              >
                {updateLimitMutation.isPending ? 'Saving...' : 'Save traffic limit'}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
