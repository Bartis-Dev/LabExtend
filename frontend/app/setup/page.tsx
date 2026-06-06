'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { api, setCSRFToken } from '@/lib/api';
import type { UserInfo } from '@/lib/types';

export default function SetupPage() {
  const router = useRouter();
  const [email, setEmail] = useState('');
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (password.length < 12) {
      setError('Password must be at least 12 characters.');
      return;
    }
    if (password !== confirm) {
      setError('Passwords do not match.');
      return;
    }
    setSubmitting(true);
    try {
      const me = await api<UserInfo>('/api/setup/initialize', {
        method: 'POST',
        body: { email, username, password, display_name: displayName },
      });
      setCSRFToken(me.csrf_token);
      router.replace('/dashboard');
    } catch (e: unknown) {
      const err = e as { body?: { error?: string } };
      setError(err?.body?.error ?? 'Setup failed.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <main className="mx-auto flex min-h-screen max-w-md flex-col justify-center px-6 py-16">
      <h1 className="text-2xl font-semibold tracking-tight">Welcome</h1>
      <p className="mt-1 text-sm text-zinc-500">Create the first admin account for this leader.</p>
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
          <span className="text-xs text-zinc-500">Username (optional, for shorter login)</span>
          <input
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            className="input mt-1"
            autoComplete="username"
            placeholder="e.g. bartis"
          />
        </label>
        <label className="block">
          <span className="text-xs text-zinc-500">Display name (optional)</span>
          <input
            type="text"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            className="input mt-1"
          />
        </label>
        <label className="block">
          <span className="text-xs text-zinc-500">Password (≥12 chars)</span>
          <input
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="input mt-1"
            autoComplete="new-password"
          />
        </label>
        <label className="block">
          <span className="text-xs text-zinc-500">Confirm password</span>
          <input
            type="password"
            required
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            className="input mt-1"
            autoComplete="new-password"
          />
        </label>

        {error && <div className="text-sm text-red-600">{error}</div>}

        <button type="submit" disabled={submitting} className="btn-primary w-full">
          {submitting ? 'Creating…' : 'Create admin & continue'}
        </button>
      </form>
    </main>
  );
}
