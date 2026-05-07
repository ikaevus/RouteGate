import { FormEvent, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { login } from '../../entities/auth/api/authApi';

export function LoginPage() {
  const [email, setEmail] = useState('admin@routegate.local');
  const [password, setPassword] = useState('admin');
  const [message, setMessage] = useState<string | null>(null);

  const loginMutation = useMutation({
    mutationFn: login,
    onSuccess: (response) => {
      localStorage.setItem('routegate.auth.token', response.token);
      setMessage(`Logged in as ${response.user.displayName}`);
    },
    onError: () => {
      setMessage('Login failed');
    },
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setMessage(null);
    loginMutation.mutate({ email, password });
  }

  return (
    <section className="page narrow-page">
      <h1>Login</h1>
      <p>Development login shell for RouteGate Manager.</p>

      <form className="auth-card" onSubmit={handleSubmit}>
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

        {message && <div className="form-message">{message}</div>}
      </form>
    </section>
  );
}
