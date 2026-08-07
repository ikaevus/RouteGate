import { FormEvent, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { changePassword } from '../../entities/auth/api/authApi';
import { t } from '../../shared/i18n/i18n';

export function SecurityPage() {
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [message, setMessage] = useState<string | null>(null);
  const [tone, setTone] = useState<'success' | 'error'>('success');

  const mutation = useMutation({
    mutationFn: changePassword,
    onSuccess: () => {
      setCurrentPassword('');
      setNewPassword('');
      setConfirmation('');
      setTone('success');
      setMessage(t('security.passwordChanged'));
    },
    onError: () => {
      setTone('error');
      setMessage(t('security.passwordChangeError'));
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

  return (
    <section className="rg101-security-page">
      <header className="rg101-security-header">
        <span>{t('security.eyebrow')}</span>
        <h1>{t('security.title')}</h1>
        <p>{t('security.description')}</p>
      </header>

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
    </section>
  );
}
