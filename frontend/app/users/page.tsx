'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { AuthShell } from '@/components/auth-shell';
import { fmtAbsTime, fmtRelative } from '@/lib/format';

interface UserView {
  id: number; email: string; display_name: string;
  is_admin: boolean; is_active: boolean; has_totp: boolean;
  created_at: number; updated_at: number; last_login_at?: number;
}

export default function UsersPage() {
  return <AuthShell><Users /></AuthShell>;
}

function Users() {
  const [users, setUsers] = useState<UserView[]>([]);
  const [creating, setCreating] = useState(false);
  const [newUser, setNewUser] = useState({ email: '', password: '', display_name: '', is_admin: false });

  const load = async () => {
    try { setUsers((await api<{ users: UserView[] }>('/api/users')).users ?? []); } catch { /* */ }
  };
  useEffect(() => { load(); }, []);

  const create = async () => {
    if (newUser.password.length < 12) { alert('password ≥ 12 chars'); return; }
    try {
      await api('/api/users', { method: 'POST', body: newUser });
      setCreating(false);
      setNewUser({ email: '', password: '', display_name: '', is_admin: false });
      load();
    } catch (e: unknown) { alert((e as { body?: { error?: string } })?.body?.error ?? String(e)); }
  };
  const toggleAdmin = async (u: UserView) => {
    await api(`/api/users/${u.id}`, { method: 'PUT', body: { is_admin: !u.is_admin } });
    load();
  };
  const toggleActive = async (u: UserView) => {
    await api(`/api/users/${u.id}`, { method: 'PUT', body: { is_active: !u.is_active } });
    load();
  };
  const resetPassword = async (u: UserView) => {
    const p = prompt(`New password for ${u.email} (≥12 chars):`);
    if (!p || p.length < 12) return;
    await api(`/api/users/${u.id}`, { method: 'PUT', body: { new_password: p } });
    alert('Password reset.');
  };
  const remove = async (u: UserView) => {
    if (!confirm(`Delete ${u.email}?`)) return;
    try {
      await api(`/api/users/${u.id}`, { method: 'DELETE' });
      load();
    } catch (e: unknown) { alert((e as { body?: { error?: string } })?.body?.error ?? String(e)); }
  };

  return (
    <div className="mx-auto max-w-5xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">Users</h1>
        <button onClick={() => setCreating(true)} className="btn-primary">New user</button>
      </header>

      <div className="card overflow-hidden p-0">
        <table className="w-full text-sm">
          <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
            <tr>
              <th className="px-4 py-2">Email</th>
              <th className="px-4 py-2">Name</th>
              <th className="px-4 py-2">Role</th>
              <th className="px-4 py-2">2FA</th>
              <th className="px-4 py-2">Status</th>
              <th className="px-4 py-2">Last login</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                <td className="px-4 py-2 font-medium">{u.email}</td>
                <td className="px-4 py-2">{u.display_name}</td>
                <td className="px-4 py-2"><span className={u.is_admin ? 'badge-green' : 'badge-zinc'}>{u.is_admin ? 'admin' : 'user'}</span></td>
                <td className="px-4 py-2"><span className={u.has_totp ? 'badge-green' : 'badge-zinc'}>{u.has_totp ? 'on' : 'off'}</span></td>
                <td className="px-4 py-2"><span className={u.is_active ? 'badge-green' : 'badge-red'}>{u.is_active ? 'active' : 'disabled'}</span></td>
                <td className="px-4 py-2 text-zinc-500" title={u.last_login_at ? fmtAbsTime(u.last_login_at) : ''}>{u.last_login_at ? fmtRelative(u.last_login_at) : '—'}</td>
                <td className="px-4 py-2 text-right">
                  <button onClick={() => toggleAdmin(u)} className="btn-ghost">{u.is_admin ? 'demote' : 'promote'}</button>
                  <button onClick={() => toggleActive(u)} className="btn-ghost">{u.is_active ? 'disable' : 'enable'}</button>
                  <button onClick={() => resetPassword(u)} className="btn-ghost">reset pw</button>
                  <button onClick={() => remove(u)} className="btn-ghost text-red-600">delete</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {creating && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setCreating(false)}>
          <div className="card w-full max-w-md" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-base font-semibold">New user</h3>
            <div className="space-y-3 text-sm">
              <label className="block">
                <span className="text-xs text-zinc-500">Email</span>
                <input className="input mt-1" type="email" value={newUser.email} onChange={(e) => setNewUser({ ...newUser, email: e.target.value })} />
              </label>
              <label className="block">
                <span className="text-xs text-zinc-500">Display name</span>
                <input className="input mt-1" value={newUser.display_name} onChange={(e) => setNewUser({ ...newUser, display_name: e.target.value })} />
              </label>
              <label className="block">
                <span className="text-xs text-zinc-500">Password (≥12 chars)</span>
                <input className="input mt-1" type="password" value={newUser.password} onChange={(e) => setNewUser({ ...newUser, password: e.target.value })} />
              </label>
              <label className="flex items-center gap-2">
                <input type="checkbox" checked={newUser.is_admin} onChange={(e) => setNewUser({ ...newUser, is_admin: e.target.checked })} />
                Admin
              </label>
            </div>
            <div className="mt-4 flex justify-end gap-2">
              <button onClick={() => setCreating(false)} className="btn-ghost">Cancel</button>
              <button onClick={create} className="btn-primary">Create</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
