'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import type { WebhookConfig } from '@/lib/types';
import { AuthShell } from '@/components/auth-shell';

export default function WebhooksPage() {
  return (
    <AuthShell>
      <Webhooks />
    </AuthShell>
  );
}

function Webhooks() {
  const [items, setItems] = useState<WebhookConfig[]>([]);
  const [editing, setEditing] = useState<Partial<WebhookConfig> | null>(null);

  const load = async () => {
    try {
      const r = await api<{ webhooks: WebhookConfig[] }>('/api/webhooks');
      setItems(r.webhooks ?? []);
    } catch {/* */}
  };

  useEffect(() => { load(); }, []);

  const save = async (w: Partial<WebhookConfig>) => {
    const payload = {
      name: w.name ?? '',
      kind: w.kind ?? 'discord',
      url: w.url ?? '',
      enabled: w.enabled ?? true,
    };
    if (w.id) await api(`/api/webhooks/${w.id}`, { method: 'PUT', body: payload });
    else await api('/api/webhooks', { method: 'POST', body: payload });
    setEditing(null);
    load();
  };
  const remove = async (id: string) => {
    if (!confirm('Delete this webhook?')) return;
    await api(`/api/webhooks/${id}`, { method: 'DELETE' });
    load();
  };
  const test = async (id: string) => {
    try {
      await api(`/api/webhooks/${id}/test`, { method: 'POST' });
      alert('Test posted. Check Discord channel.');
    } catch (e) { alert('Test failed: ' + String(e)); }
  };

  return (
    <div className="mx-auto max-w-5xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">Webhooks</h1>
        <button onClick={() => setEditing({ enabled: true, kind: 'discord' })} className="btn-primary">New webhook</button>
      </header>

      <div className="card overflow-hidden p-0">
        <table className="w-full text-sm">
          <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
            <tr>
              <th className="px-4 py-2">Name</th>
              <th className="px-4 py-2">Kind</th>
              <th className="px-4 py-2">URL</th>
              <th className="px-4 py-2">Status</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {items.map((w) => (
              <tr key={w.id} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                <td className="px-4 py-2 font-medium">{w.name}</td>
                <td className="px-4 py-2 text-zinc-500">{w.kind}</td>
                <td className="px-4 py-2 font-mono text-[11px] text-zinc-500">{w.url}</td>
                <td className="px-4 py-2"><span className={w.enabled ? 'badge-green' : 'badge-zinc'}>{w.enabled ? 'enabled' : 'disabled'}</span></td>
                <td className="px-4 py-2 text-right">
                  <button onClick={() => test(w.id)} className="btn-ghost">test</button>
                  <button onClick={() => setEditing(w)} className="btn-ghost">edit</button>
                  <button onClick={() => remove(w.id)} className="btn-ghost text-red-600">delete</button>
                </td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td colSpan={5} className="px-4 py-6 text-center text-sm text-zinc-500">No webhooks. Add a Discord webhook to receive alerts.</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setEditing(null)}>
          <div className="card w-full max-w-md" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-base font-semibold">{editing.id ? 'Edit webhook' : 'New webhook'}</h3>
            <div className="space-y-3">
              <label className="block">
                <span className="text-xs text-zinc-500">Name</span>
                <input className="input mt-1" value={editing.name ?? ''} onChange={(e) => setEditing({ ...editing, name: e.target.value })} />
              </label>
              <label className="block">
                <span className="text-xs text-zinc-500">Kind</span>
                <select className="input mt-1" value={editing.kind ?? 'discord'} onChange={(e) => setEditing({ ...editing, kind: e.target.value })}>
                  <option value="discord">discord</option>
                </select>
              </label>
              <label className="block">
                <span className="text-xs text-zinc-500">URL {editing.id && '(leave masked to keep)'}</span>
                <input className="input mt-1 font-mono text-[11px]" placeholder="https://discord.com/api/webhooks/…/…" value={editing.url ?? ''} onChange={(e) => setEditing({ ...editing, url: e.target.value })} />
              </label>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={editing.enabled ?? true} onChange={(e) => setEditing({ ...editing, enabled: e.target.checked })} />
                Enabled
              </label>
            </div>
            <div className="mt-4 flex justify-end gap-2">
              <button onClick={() => setEditing(null)} className="btn-ghost">Cancel</button>
              <button onClick={() => save(editing)} className="btn-primary">Save</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
