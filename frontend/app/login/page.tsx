'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { api, setCSRFToken } from '@/lib/api';
import type { UserInfo } from '@/lib/types';

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      const me = await api<UserInfo>('/api/auth/login', {
        method: 'POST',
        body: { email, password },
      });
      setCSRFToken(me.csrf_token);
      router.replace('/dashboard');
    } catch (e: unknown) {
      const err = e as { body?: { error?: string }; status?: number };
      setError(err?.body?.error ?? 'Invalid credentials.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <main className="mx-auto flex min-h-screen max-w-sm flex-col justify-center px-6 py-16">
      <h1 className="text-2xl font-semibold tracking-tight">Sign in</h1>
      <p className="mt-1 text-sm text-zinc-500">Access the labextend dashboard.</p>
      <form onSubmit={onSubmit} className="mt-6 space-y-3">
        <label className="block">
          <span className="text-xs text-zinc-500">Email</span>
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="input mt-1"
            autoComplete="email"
          />
        </label>
        <label className="block">
          <span className="text-xs text-zinc-500">Password</span>
          <input
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="input mt-1"
            autoComplete="current-password"
          />
        </label>

        {error && <div className="text-sm text-red-600">{error}</div>}

        <button type="submit" disabled={submitting} className="btn-primary w-full">
          {submitting ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </main>
  );
}
