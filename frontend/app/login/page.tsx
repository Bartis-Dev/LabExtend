'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { api, setCSRFToken } from '@/lib/api';
import type { UserInfo } from '@/lib/types';

export default function LoginPage() {
  const router = useRouter();
  const [step, setStep] = useState<'creds' | '2fa'>('creds');
  const [identifier, setIdentifier] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [isRecovery, setIsRecovery] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sso, setSso] = useState<{ enabled: boolean; name: string; password_login: boolean } | null>(null);

  useEffect(() => {
    api<{ enabled: boolean; name: string; password_login: boolean }>('/api/auth/oidc/config')
      .then(setSso)
      .catch(() => setSso({ enabled: false, name: '', password_login: true }));
    // A failed OIDC callback lands here with the reason in the query string.
    const q = new URLSearchParams(window.location.search);
    const err = q.get('oidc_error');
    if (err) {
      setError(err);
      window.history.replaceState(null, '', window.location.pathname);
    }
  }, []);

  const showPassword = !sso || sso.password_login || !sso.enabled;

  const onCreds = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      const me = await api<UserInfo & { requires_2fa?: boolean }>('/api/auth/login', {
        method: 'POST', body: { identifier, password },
      });
      setCSRFToken(me.csrf_token);
      if (me.requires_2fa) {
        setStep('2fa');
      } else {
        router.replace('/nodes');
      }
    } catch (e: unknown) {
      const err = e as { body?: { error?: string } };
      setError(err?.body?.error ?? 'Invalid credentials.');
    } finally {
      setSubmitting(false);
    }
  };

  const on2FA = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await api('/api/auth/2fa/verify', {
        method: 'POST', body: { code, is_recovery: isRecovery },
      });
      router.replace('/nodes');
    } catch (e: unknown) {
      const err = e as { body?: { error?: string } };
      setError(err?.body?.error ?? 'Invalid code.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <main className="mx-auto flex min-h-screen max-w-sm flex-col justify-center px-6 py-16">
      <h1 className="text-2xl font-semibold tracking-tight">Sign in</h1>
      <p className="mt-1 text-sm text-zinc-500">Access the LabExtend dashboard.</p>

      {step === 'creds' && sso?.enabled && (
        <div className="mt-6 space-y-3">
          {/* A plain link: the endpoint answers with a redirect to the provider. */}
          <a href="/api/auth/oidc/login" className="btn-primary w-full">
            Sign in with {sso.name}
          </a>
          {showPassword && (
            <div className="flex items-center gap-3 text-xs text-zinc-400">
              <span className="h-px flex-1 bg-zinc-200 dark:bg-zinc-800" />
              or use your password
              <span className="h-px flex-1 bg-zinc-200 dark:bg-zinc-800" />
            </div>
          )}
          {!showPassword && error && <div role="alert" className="text-sm text-red-600">{error}</div>}
        </div>
      )}

      {step === 'creds' ? (showPassword && (
        <form onSubmit={onCreds} className={sso?.enabled ? 'mt-3 space-y-3' : 'mt-6 space-y-3'}>
          <label className="block">
            <span className="text-xs text-zinc-500">Email or username</span>
            <input
              type="text"
              required
              value={identifier}
              onChange={(e) => setIdentifier(e.target.value)}
              className="input mt-1"
              autoComplete="username"
              autoFocus
            />
          </label>
          <label className="block">
            <span className="text-xs text-zinc-500">Password</span>
            <input type="password" required value={password} onChange={(e) => setPassword(e.target.value)} className="input mt-1" autoComplete="current-password" />
          </label>
          {error && <div className="text-sm text-red-600">{error}</div>}
          <button type="submit" disabled={submitting} className="btn-primary w-full">
            {submitting ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      )) : (
        <form onSubmit={on2FA} className="mt-6 space-y-3">
          <label className="block">
            <span className="text-xs text-zinc-500">{isRecovery ? 'Recovery code' : '6-digit TOTP code'}</span>
            <input
              required
              value={code}
              onChange={(e) => setCode(e.target.value)}
              className="input mt-1 font-mono"
              maxLength={isRecovery ? 12 : 6}
              autoFocus
            />
          </label>
          <label className="flex items-center gap-2 text-xs text-zinc-500">
            <input type="checkbox" checked={isRecovery} onChange={(e) => setIsRecovery(e.target.checked)} />
            Use recovery code instead
          </label>
          {error && <div className="text-sm text-red-600">{error}</div>}
          <button type="submit" disabled={submitting} className="btn-primary w-full">
            {submitting ? 'Verifying…' : 'Verify'}
          </button>
          <button type="button" onClick={() => { setStep('creds'); setCode(''); setError(null); }} className="btn-ghost w-full">
            Back
          </button>
        </form>
      )}
    </main>
  );
}
