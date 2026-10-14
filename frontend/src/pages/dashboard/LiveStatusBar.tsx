import { useEffect, useState } from 'react';
import { t } from '../../shared/i18n/i18n';

const STALE_AFTER_MS = 30_000;

// Shows that the overview is polling: a pulsing dot while fresh data keeps
// arriving and how long ago the latest refresh landed.
export function LiveStatusBar({ updatedAt, serverTime, serverTimeReceivedAt }: { updatedAt: number; serverTime?: string; serverTimeReceivedAt: number }) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const age = updatedAt > 0 ? Math.max(now - updatedAt, 0) : Number.POSITIVE_INFINITY;
  const live = age < STALE_AFTER_MS;
  const seconds = Math.floor(age / 1000);
  const updated = !live
    ? t('dashboard.liveStale')
    : seconds < 2 ? t('dashboard.liveUpdatedNow') : t('dashboard.liveUpdated', { seconds });
  // The Manager reports its time on each health poll; advance it locally between polls.
  const time = serverTime ? new Date(new Date(serverTime).getTime() + Math.max(now - serverTimeReceivedAt, 0)) : null;

  return (
    <div className={`dashboard-live-bar${live ? ' dashboard-live-bar--live' : ''}`}>
      <span className="dashboard-live-indicator"><i aria-hidden="true" />{t('dashboard.live')}</span>
      <span className="dashboard-live-updated">{updated}</span>
      {time && !Number.isNaN(time.getTime()) && (
        <span className="dashboard-live-time">{t('dashboard.serverTime', { time: time.toLocaleTimeString() })}</span>
      )}
    </div>
  );
}
