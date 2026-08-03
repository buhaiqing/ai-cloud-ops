'use client';
import { useState, FormEvent } from 'react';
import { login } from '../../lib/auth';

export default function LoginPage() {
  const [user, setUser] = useState('');
  const [pass, setPass] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      await login(user, pass);
      // Hard reload so server-side state (cookies) is fresh.
      window.location.href = '/';
    } catch (e) {
      // 401 and 400 both surface as the same generic message — don't
      // differentiate (per security review: avoid user enumeration).
      setErr('Invalid credentials');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="max-w-sm mx-auto mt-12">
      <h1 className="text-2xl font-bold mb-6">Sign in</h1>
      <form data-testid="login-form" onSubmit={onSubmit} className="space-y-4">
        <div>
          <label className="block text-sm mb-1" htmlFor="input-user">Username</label>
          <input
            id="input-user"
            data-testid="input-user"
            type="text"
            autoComplete="username"
            value={user}
            onChange={(e) => setUser(e.target.value)}
            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 rounded bg-white dark:bg-gray-900"
            required
          />
        </div>
        <div>
          <label className="block text-sm mb-1" htmlFor="input-pass">Password</label>
          <input
            id="input-pass"
            data-testid="input-pass"
            type="password"
            autoComplete="current-password"
            value={pass}
            onChange={(e) => setPass(e.target.value)}
            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 rounded bg-white dark:bg-gray-900"
            required
          />
        </div>
        {err && (
          <div data-testid="login-error" className="text-red-500 text-sm">{err}</div>
        )}
        <button
          data-testid="btn-login"
          type="submit"
          disabled={busy}
          className="w-full px-3 py-2 bg-blue-600 text-white rounded disabled:opacity-50"
        >
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </section>
  );
}