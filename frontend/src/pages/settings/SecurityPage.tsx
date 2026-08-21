import { FormEvent, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  changePassword,
  clearSecurityEvents,
  getSecurityEvents,
  getSecuritySessions,
  revokeOtherSecuritySessions,
  revokeSecuritySession,
  type SecurityEvent,
  type SecuritySession,
} from '../../entities/auth/api/authApi';
import { t } from '../../shared/i18n/i18n';
import { useLocale } from '../../shared/i18n/useLocale';

const copy = {
  en: {
    overview: 'Account protection',
    password: 'Password',
    passwordConfigured: 'Configured',
    twoFactor: 'Two-factor authentication',
    twoFactorPlanned: 'Not configured',
    activeSessions: 'Active sessions',
    sessionsDescription: 'Browsers and devices that currently have an authenticated RouteGate session.',
    currentSession: 'Current session',
    revoke: 'End session',
    revokeAll: 'End all other sessions',
    revoking: 'Ending session...',
    sessionsLoading: 'Loading active sessions...',
    sessionsError: 'Active sessions could not be loaded.',
    noOtherSessions: 'No other active sessions.',
    lastActive: 'Last active',
    created: 'Created',
    expires: 'Expires',
    unknownDevice: 'Unknown browser or device',
    unknownAddress: 'IP address unavailable',
    events: 'Recent security activity',
    eventsDescription: 'Authentication and account-security events recorded for this administrator.',
    eventsLoading: 'Loading security activity...',
    eventsError: 'Security activity could not be loaded.',
    noEvents: 'No security events have been recorded since the history was cleared.',
    clearEvents: 'Clear history',
    clearingEvents: 'Clearing...',
    clearEventsError: 'Security history could not be cleared.',
    clearEventsHint: 'Clears this on-screen history without deleting the immutable audit trail.',
    eventLogin: 'Signed in',
    eventLogout: 'Signed out',
    eventPassword: 'Password changed',
    eventSessionRevoked: 'Session ended',
    eventSessionsRevoked: 'Other sessions ended',
    eventSetup: 'Initial account setup completed',
    eventOther: 'Security event',
    success: 'Success',
    failure: 'Failed',
    twoFactorNote: 'Two-factor authentication will be added only together with recovery codes and a complete server-side implementation.',
  },
  ru: {
    overview: 'Защита аккаунта',
    password: 'Пароль',
    passwordConfigured: 'Настроен',
    twoFactor: 'Двухфакторная аутентификация',
    twoFactorPlanned: 'Не настроена',
    activeSessions: 'Активные сессии',
    sessionsDescription: 'Браузеры и устройства, в которых сейчас открыта авторизованная сессия RouteGate.',
    currentSession: 'Текущая сессия',
    revoke: 'Завершить сессию',
    revokeAll: 'Завершить все другие сессии',
    revoking: 'Завершение сессии...',
    sessionsLoading: 'Загрузка активных сессий...',
    sessionsError: 'Не удалось загрузить активные сессии.',
    noOtherSessions: 'Других активных сессий нет.',
    lastActive: 'Последняя активность',
    created: 'Создана',
    expires: 'Истекает',
    unknownDevice: 'Неизвестный браузер или устройство',
    unknownAddress: 'IP-адрес недоступен',
    events: 'Последние события безопасности',
    eventsDescription: 'События аутентификации и защиты аккаунта, записанные для этого администратора.',
    eventsLoading: 'Загрузка событий безопасности...',
    eventsError: 'Не удалось загрузить события безопасности.',
    noEvents: 'После очистки новые события безопасности пока не записаны.',
    clearEvents: 'Очистить историю',
    clearingEvents: 'Очистка...',
    clearEventsError: 'Не удалось очистить историю безопасности.',
    clearEventsHint: 'Очищает эту историю на экране, не удаляя неизменяемый журнал аудита.',
    eventLogin: 'Выполнен вход',
    eventLogout: 'Выполнен выход',
    eventPassword: 'Пароль изменён',
    eventSessionRevoked: 'Сессия завершена',
    eventSessionsRevoked: 'Другие сессии завершены',
    eventSetup: 'Первичная настройка аккаунта завершена',
    eventOther: 'Событие безопасности',
    success: 'Успешно',
    failure: 'Ошибка',
    twoFactorNote: 'Двухфакторная аутентификация появится только вместе с кодами восстановления и полноценной серверной реализацией.',
  },
} as const;

function summarizeUserAgent(userAgent: string | undefined, fallback: string): string {
  if (!userAgent) return fallback;

  const browser = userAgent.includes('Edg/')
    ? 'Edge'
    : userAgent.includes('Chrome/')
      ? 'Chrome'
      : userAgent.includes('Firefox/')
        ? 'Firefox'
        : userAgent.includes('Safari/')
          ? 'Safari'
          : 'Browser';

  const platform = userAgent.includes('Windows')
    ? 'Windows'
    : userAgent.includes('iPhone') || userAgent.includes('iPad')
      ? 'iOS / iPadOS'
      : userAgent.includes('Android')
        ? 'Android'
        : userAgent.includes('Macintosh')
          ? 'macOS'
          : userAgent.includes('Linux')
            ? 'Linux'
            : '';

  return platform ? `${browser} · ${platform}` : browser;
}

function eventLabel(event: SecurityEvent, c: typeof copy.en | typeof copy.ru): string {
  switch (event.action) {
    case 'auth.login.success': return c.eventLogin;
    case 'auth.logout.success': return c.eventLogout;
    case 'auth.password.changed': return c.eventPassword;
    case 'auth.session.revoked': return c.eventSessionRevoked;
    case 'auth.sessions.revoked_others': return c.eventSessionsRevoked;
    case 'auth.initial_setup.completed': return c.eventSetup;
    default: return c.eventOther;
  }
}

function SessionRow({
  session,
  onRevoke,
  isRevoking,
  c,
  formatDate,
}: {
  session: SecuritySession;
  onRevoke: (id: string) => void;
  isRevoking: boolean;
  c: typeof copy.en | typeof copy.ru;
  formatDate: (value: string) => string;
}) {
  return (
    <div className="panel rg101-security-card">
      <div>
        <h3>{summarizeUserAgent(session.user_agent, c.unknownDevice)}</h3>
        <p>{session.ip_address || c.unknownAddress}</p>
      </div>
      {session.current && <strong>{c.currentSession}</strong>}
      <div className="rg110-security-meta">
        <span>{c.lastActive}: {formatDate(session.last_used_at || session.created_at)}</span>
        <span>{c.created}: {formatDate(session.created_at)}</span>
        <span>{c.expires}: {formatDate(session.expires_at)}</span>
      </div>
      {!session.current && (
        <button className="small-button" type="button" disabled={isRevoking} onClick={() => onRevoke(session.id)}>
          {isRevoking ? c.revoking : c.revoke}
        </button>
      )}
    </div>
  );
}

export function SecurityPage() {
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [message, setMessage] = useState<string | null>(null);
  const [tone, setTone] = useState<'success' | 'error'>('success');
  const queryClient = useQueryClient();
  const { locale } = useLocale();
  const c = copy[locale];

  const formatDate = useMemo(() => {
    const formatter = new Intl.DateTimeFormat(locale === 'ru' ? 'ru-RU' : 'en-US', {
      dateStyle: 'medium',
      timeStyle: 'short',
    });
    return (value: string) => formatter.format(new Date(value));
  }, [locale]);

  const sessionsQuery = useQuery({
    queryKey: ['security-sessions'],
    queryFn: getSecuritySessions,
  });
  const eventsQuery = useQuery({
    queryKey: ['security-events'],
    queryFn: getSecurityEvents,
  });

  const mutation = useMutation({
    mutationFn: changePassword,
    onSuccess: () => {
      setCurrentPassword('');
      setNewPassword('');
      setConfirmation('');
      setTone('success');
      setMessage(t('security.passwordChanged'));
      queryClient.invalidateQueries({ queryKey: ['security-sessions'] });
      queryClient.invalidateQueries({ queryKey: ['security-events'] });
    },
    onError: () => {
      setTone('error');
      setMessage(t('security.passwordChangeError'));
    },
  });

  const revokeMutation = useMutation({
    mutationFn: revokeSecuritySession,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['security-sessions'] });
      queryClient.invalidateQueries({ queryKey: ['security-events'] });
    },
  });

  const revokeOthersMutation = useMutation({
    mutationFn: revokeOtherSecuritySessions,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['security-sessions'] });
      queryClient.invalidateQueries({ queryKey: ['security-events'] });
    },
  });

  const clearEventsMutation = useMutation({
    mutationFn: clearSecurityEvents,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['security-events'] });
    },
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setMessage(null);
    if (newPassword.length < 12) {
      setTone('error');
      setMessage(t('setup.passwordTooShort'));
      return;
    }
    if (newPassword !== confirmation) {
      setTone('error');
      setMessage(t('setup.passwordMismatch'));
      return;
    }
    mutation.mutate({
      current_password: currentPassword,
      new_password: newPassword,
    });
  }

  const sessions = sessionsQuery.data?.sessions ?? [];
  const otherSessionCount = sessions.filter((session) => !session.current).length;
  const securityEvents = eventsQuery.data?.events ?? [];

  return (
    <section className="rg101-security-page">
      <header className="rg101-security-header">
        <span>{t('security.eyebrow')}</span>
        <h1>{t('security.title')}</h1>
        <p>{t('security.description')}</p>
      </header>

      <div className="panel rg101-security-card">
        <div>
          <h2>{c.overview}</h2>
          <p>{c.password}: <strong>{c.passwordConfigured}</strong></p>
          <p>{c.twoFactor}: <strong>{c.twoFactorPlanned}</strong></p>
          <p>{c.activeSessions}: <strong>{sessionsQuery.isSuccess ? sessions.length : '—'}</strong></p>
        </div>
        <p className="rg101-password-hint">{c.twoFactorNote}</p>
      </div>

      <section>
        <div className="rg101-security-header">
          <h2>{c.activeSessions}</h2>
          <p>{c.sessionsDescription}</p>
        </div>

        {sessionsQuery.isPending && <p className="empty-state">{c.sessionsLoading}</p>}
        {sessionsQuery.isError && <p className="form-message auth-message auth-message-error">{c.sessionsError}</p>}
        {sessionsQuery.isSuccess && sessions.map((session) => (
          <SessionRow
            key={session.id}
            session={session}
            onRevoke={(id) => revokeMutation.mutate(id)}
            isRevoking={revokeMutation.isPending && revokeMutation.variables === session.id}
            c={c}
            formatDate={formatDate}
          />
        ))}
        {sessionsQuery.isSuccess && otherSessionCount === 0 && <p className="rg101-password-hint">{c.noOtherSessions}</p>}
        {otherSessionCount > 0 && (
          <button
            className="small-button"
            type="button"
            disabled={revokeOthersMutation.isPending}
            onClick={() => revokeOthersMutation.mutate()}
          >
            {revokeOthersMutation.isPending ? c.revoking : c.revokeAll}
          </button>
        )}
      </section>

      <form className="panel rg101-security-card" onSubmit={handleSubmit}>
        <div>
          <h2>{t('security.changePassword')}</h2>
          <p>{t('security.changePasswordDescription')}</p>
        </div>

        <label className="field">
          <span>{t('security.currentPassword')}</span>
          <input
            type="password"
            autoComplete="current-password"
            value={currentPassword}
            onChange={(event) => setCurrentPassword(event.target.value)}
            required
          />
        </label>

        <label className="field">
          <span>{t('security.newPassword')}</span>
          <input
            type="password"
            autoComplete="new-password"
            value={newPassword}
            onChange={(event) => setNewPassword(event.target.value)}
            minLength={12}
            maxLength={128}
            required
          />
        </label>

        <label className="field">
          <span>{t('security.confirmPassword')}</span>
          <input
            type="password"
            autoComplete="new-password"
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
            minLength={12}
            maxLength={128}
            required
          />
        </label>

        <p className="rg101-password-hint">{t('security.sessionHint')}</p>

        <button className="primary-button" type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? t('security.changing') : t('security.changeAction')}
        </button>

        {message && <div className={`form-message auth-message auth-message-${tone}`}>{message}</div>}
      </form>

      <section>
        <div className="rg101-security-section-heading">
          <div className="rg101-security-header">
            <h2>{c.events}</h2>
            <p>{c.eventsDescription}</p>
          </div>
          {securityEvents.length > 0 && (
            <button
              className="small-button"
              type="button"
              disabled={clearEventsMutation.isPending}
              onClick={() => clearEventsMutation.mutate()}
            >
              {clearEventsMutation.isPending ? c.clearingEvents : c.clearEvents}
            </button>
          )}
        </div>
        {securityEvents.length > 0 && <p className="rg101-password-hint rg101-security-clear-hint">{c.clearEventsHint}</p>}
        {eventsQuery.isPending && <p className="empty-state">{c.eventsLoading}</p>}
        {eventsQuery.isError && <p className="form-message auth-message auth-message-error">{c.eventsError}</p>}
        {clearEventsMutation.isError && <p className="form-message auth-message auth-message-error">{c.clearEventsError}</p>}
        {eventsQuery.isSuccess && securityEvents.length === 0 && <p className="empty-state">{c.noEvents}</p>}
        {securityEvents.map((event) => (
          <div className="panel rg101-security-card" key={event.id}>
            <div>
              <h3>{eventLabel(event, c)}</h3>
              <p>{formatDate(event.created_at)}</p>
            </div>
            <strong>{event.result === 'success' ? c.success : c.failure}</strong>
          </div>
        ))}
      </section>
    </section>
  );
}
