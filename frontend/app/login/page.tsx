'use client';

import { useState } from 'react';
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
      <p className="mt-1 text-sm text-zinc-500">Access the labextend dashboard.</p>

      {step === 'creds' ? (
        <form onSubmit={onCreds} className="mt-6 space-y-3">
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
      ) : (
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
