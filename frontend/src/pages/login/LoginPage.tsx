import { FormEvent, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { login } from '../../entities/auth/api/authApi';

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
      setMessage(`Logged in as ${response.user.displayName}`);
    },
    onError: () => {
      setMessageTone('error');
      setMessage('Login failed. Check Manager availability and try again.');
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
        <span className="auth-eyebrow">Admin access</span>
        <h1>Sign in to RouteGate</h1>
        <p>
          Access the RouteGate Manager console to manage VPN accounts, servers, agents,
          routing profiles, and configuration deployment.
        </p>

        <div className="auth-signal-grid" aria-hidden="true">
          <div><strong>Manager</strong><span>Control plane</span></div>
          <div><strong>Agents</strong><span>Node fleet</span></div>
          <div><strong>VPN</strong><span>Accounts & routes</span></div>
        </div>
      </div>

      <form className="auth-card auth-login-card" onSubmit={handleSubmit}>
        <div className="auth-card-header">
          <span className="auth-eyebrow">Manager login</span>
          <h2>Admin Console</h2>
          <p>Open the Manager session for the current environment.</p>
        </div>

        <label className="field">
          <span>Email</span>
          <input value={email} onChange={(event) => setEmail(event.target.value)} />
        </label>

        <label className="field">
          <span>Password</span>
          <input
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </label>

        <button className="primary-button" type="submit" disabled={loginMutation.isPending}>
          {loginMutation.isPending ? 'Signing in...' : 'Sign in'}
        </button>

        {message && <div className={`form-message auth-message auth-message-${messageTone}`}>{message}</div>}
      </form>
    </section>
  );
}
