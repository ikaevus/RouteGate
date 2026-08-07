import { FormEvent, useMemo, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import {
  completeInitialSetup,
  inspectInitialSetup,
} from '../../entities/auth/api/authApi';
import { setAuthToken } from '../../shared/api/client';
import { t } from '../../shared/i18n/i18n';

interface SetupPageProps {
  onLogin?: () => void;
}

function readSetupToken(): string {
  if (typeof window === 'undefined') {
    return '';
  }
  const fragment = window.location.hash.startsWith('#')
    ? window.location.hash.slice(1)
    : window.location.hash;
  return new URLSearchParams(fragment).get('token')?.trim() ?? '';
}

export function SetupPage({ onLogin }: SetupPageProps) {
  const navigate = useNavigate();
  const token = useMemo(readSetupToken, []);
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [message, setMessage] = useState<string | null>(null);

  const inspectQuery = useQuery({
    queryKey: ['initial-setup', token],
    queryFn: () => inspectInitialSetup({ token }),
    enabled: token !== '',
    retry: false,
  });

  const completeMutation = useMutation({
    mutationFn: completeInitialSetup,
    onSuccess: (response) => {
      setAuthToken(response.token);
      window.history.replaceState(null, '', '/setup');
      onLogin?.();
      navigate('/', { replace: true });
    },
    onError: () => setMessage(t('setup.completeError')),
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setMessage(null);
    if (password.length < 12) {
      setMessage(t('setup.passwordTooShort'));
      return;
    }
    if (password !== confirmation) {
      setMessage(t('setup.passwordMismatch'));
      return;
    }
    completeMutation.mutate({ token, new_password: password });
  }

  const checking = token !== '' && inspectQuery.isPending;
  const invalid = token === '' || inspectQuery.isError;

  return (
    <section className="auth-page rg101-setup-page">
      <div className="auth-hero-panel">
        <span className="auth-eyebrow">{t('setup.eyebrow')}</span>
        <h1>{t('setup.title')}</h1>
        <p>{t('setup.description')}</p>
        <div className="auth-signal-grid" aria-hidden="true">
          <div><strong>{t('setup.stepIdentity')}</strong><span>{t('setup.stepIdentityMeta')}</span></div>
          <div><strong>{t('setup.stepPassword')}</strong><span>{t('setup.stepPasswordMeta')}</span></div>
          <div><strong>{t('setup.stepContinue')}</strong><span>{t('setup.stepContinueMeta')}</span></div>
        </div>
      </div>

      <form className="auth-card auth-login-card rg101-setup-card" onSubmit={handleSubmit}>
        <div className="auth-card-header">
          <span className="auth-eyebrow">{t('setup.cardEyebrow')}</span>
          <h2>{t('setup.cardTitle')}</h2>
          <p>{t('setup.cardDescription')}</p>
        </div>

        {checking && (
          <div className="form-message">{t('setup.checking')}</div>
        )}

        {invalid && !checking && (
          <div className="form-message auth-message auth-message-error">{t('setup.invalidLink')}</div>
        )}

        {!invalid && inspectQuery.data && (
          <>
            <label className="field">
              <span>{t('setup.email')}</span>
              <input type="email" value={inspectQuery.data.email} readOnly autoComplete="username" />
            </label>

            <label className="field">
              <span>{t('setup.newPassword')}</span>
              <input
                type="password"
                autoComplete="new-password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                minLength={12}
                maxLength={128}
                required
              />
            </label>

            <label className="field">
              <span>{t('setup.confirmPassword')}</span>
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

            <p className="rg101-password-hint">{t('setup.passwordHint')}</p>

            <button className="primary-button" type="submit" disabled={completeMutation.isPending}>
              {completeMutation.isPending ? t('setup.completing') : t('setup.complete')}
            </button>
          </>
        )}

        {message && <div className="form-message auth-message auth-message-error">{message}</div>}
      </form>
    </section>
  );
}
