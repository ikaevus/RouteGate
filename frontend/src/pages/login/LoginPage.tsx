import { FormEvent, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { login } from '../../entities/auth/api/authApi';
import { setAuthToken } from '../../shared/api/client';
import { t } from '../../shared/i18n/i18n';

interface LoginPageProps {
  onLogin?: () => void;
}

function safeReturnTo(value: string | null): string {
  if (!value || !value.startsWith('/') || value.startsWith('//')) {
    return '/';
  }

  return value;
}

export function LoginPage({ onLogin }: LoginPageProps) {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [message, setMessage] = useState<string | null>(null);
  const [messageTone, setMessageTone] = useState<'success' | 'error'>('success');
  const returnTo = safeReturnTo(searchParams.get('returnTo'));

  const loginMutation = useMutation({
    mutationFn: login,
    onSuccess: (response) => {
      setAuthToken(response.token);
      onLogin?.();
      navigate(returnTo, { replace: true });
    },
    onError: () => {
      setMessageTone('error');
      setMessage(t('login.failure'));
    },
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setMessage(null);
    loginMutation.mutate({ email, password });
  }

  return (
    <section className="auth-page">
      <div className="auth-hero-panel">
        <span className="auth-eyebrow">{t('login.adminAccess')}</span>
        <h1>{t('login.title')}</h1>
        <p>
          {t('login.description')}
        </p>

        <div className="auth-signal-grid" aria-hidden="true">
          <div><strong>{t('login.manager')}</strong><span>{t('login.controlPlane')}</span></div>
          <div><strong>{t('login.agents')}</strong><span>{t('login.nodeFleet')}</span></div>
          <div><strong>{t('login.vpn')}</strong><span>{t('login.accountsAndRoutes')}</span></div>
        </div>
      </div>

      <form className="auth-card auth-login-card" onSubmit={handleSubmit}>
        <div className="auth-card-header">
          <span className="auth-eyebrow">{t('login.managerLogin')}</span>
          <h2>{t('login.adminConsole')}</h2>
          <p>{t('login.sessionDescription')}</p>
        </div>

        <label className="field">
          <span>{t('login.email')}</span>
          <input
            type="email"
            autoComplete="username"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            required
          />
        </label>

        <label className="field">
          <span>{t('login.password')}</span>
          <input
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            required
          />
        </label>

        <button className="primary-button" type="submit" disabled={loginMutation.isPending}>
          {loginMutation.isPending ? t('login.signingIn') : t('login.signIn')}
        </button>

        {message && <div className={`form-message auth-message auth-message-${messageTone}`}>{message}</div>}
      </form>
    </section>
  );
}