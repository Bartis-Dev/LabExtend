'use client';

import { ExternalLink, LogOut } from 'lucide-react';
import { api, setCSRFToken } from '@/lib/api';
import { AuthShell, useMe } from '@/components/auth-shell';

export default function AccountPage() {
  return <AuthShell><Account /></AuthShell>;
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 border-b border-zinc-100 py-2.5 last:border-0 dark:border-zinc-800">
      <dt className="text-xs text-zinc-500">{label}</dt>
      <dd className="text-right text-sm">{children}</dd>
    </div>
  );
}

function Account() {
  const me = useMe();
  const group = me.is_admin ? me.admin_group : me.user_group;

  const logout = async () => {
    try {
      await api('/api/auth/logout', { method: 'POST' });
    } catch { /* the cookie is cleared server-side; go to login either way */ }
    setCSRFToken(null);
    window.location.href = '/login';
  };

  return (
    <div className="page-narrow">
      <h1 className="page-title">Account</h1>
      <p className="page-sub">
        Your account is managed in {me.provider_name}. Changes there apply on your next sign-in.
      </p>

      <section className="card mt-6">
        <dl>
          <Row label="Name">{me.display_name || '-'}</Row>
          {me.username && <Row label="Username"><span className="font-mono text-sm">{me.username}</span></Row>}
          <Row label="Email">{me.email}</Row>
          <Row label="Role">
            <span className={me.is_admin ? 'badge-green' : 'badge-zinc'}>{me.is_admin ? 'Admin' : 'User'}</span>
            <span className="ml-2 text-xs text-zinc-500">
              from group <span className="font-mono">{group}</span>
            </span>
          </Row>
        </dl>
      </section>

      <section className="card mt-6">
        <h2 className="section-title mb-1">Passkeys and profile</h2>
        <p className="mb-3 text-sm text-zinc-600 dark:text-zinc-400">
          Add or remove passkeys and edit your name or email in {me.provider_name}.
          Admin rights follow membership in the <span className="font-mono">{me.admin_group}</span> group.
        </p>
        <div className="flex flex-wrap gap-2">
          {me.provider_url && (
            <a href={me.provider_url} target="_blank" rel="noopener noreferrer" className="btn-primary">
              Open {me.provider_name} <ExternalLink className="h-3.5 w-3.5" />
            </a>
          )}
          <button type="button" onClick={logout} className="btn-ghost">
            <LogOut className="h-4 w-4" /> Sign out
          </button>
        </div>
      </section>
    </div>
  );
}
