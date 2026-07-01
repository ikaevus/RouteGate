import { FormEvent, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { login } from '../../entities/auth/api/authApi';
import { t } from '../../shared/i18n/i18n';

export function LoginPage() {
  const [email, setEmail] = useState('admin@routegate.local');
  const [password, setPassword] = useState('admin');
  const [message, setMessage] = useState<string | null>(null);
  const [messageTone, setMessageTone] = useState<'success' | 'error'>('success');

  const loginMutation = useMutation({
    mutationFn: login,
    onSuccess: (response) => {
      localStorage.setItem('routegate.auth.token', response.token);
      setMessageTone('success');
      setMessage(t('login.success', { name: response.user.displayName }));
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
          <input value={email} onChange={(event) => setEmail(event.target.value)} />
        </label>

        <label className="field">
          <span>{t('login.password')}</span>
          <input
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
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
