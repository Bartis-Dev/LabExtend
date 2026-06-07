'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { AuthShell } from '@/components/auth-shell';
import type { NodeView } from '@/lib/types';
import { SchedulePicker } from '@/components/schedule-picker';

interface CronJob {
  id: string;
  node_id: string;
  schedule: string;
  command: string;
  run_as: string;
  comment: string;
  enabled: boolean;
  created_at: number;
  updated_at: number;
}

export default function CronJobsPage() {
  return <AuthShell><Cron /></AuthShell>;
}

function Cron() {
  const [nodes, setNodes] = useState<NodeView[]>([]);
  const [jobs, setJobs] = useState<CronJob[]>([]);
  const [editing, setEditing] = useState<Partial<CronJob> | null>(null);
  const [filterNode, setFilterNode] = useState('');

  const load = async () => {
    try {
      const n = await api<{ nodes: NodeView[] }>('/api/nodes');
      setNodes(n.nodes ?? []);
      const url = filterNode ? `/api/nodes/${encodeURIComponent(filterNode)}/cronjobs` : '/api/cronjobs';
      const j = await api<{ jobs: CronJob[] }>(url);
      setJobs(j.jobs ?? []);
    } catch { /* */ }
  };
  useEffect(() => { load(); }, [filterNode]);

  const save = async (j: Partial<CronJob>) => {
    const payload = {
      node_id: j.node_id ?? '', schedule: j.schedule ?? '', command: j.command ?? '',
      run_as: j.run_as || 'root', comment: j.comment ?? '', enabled: j.enabled ?? true,
    };
    if (j.id) await api(`/api/cronjobs/${j.id}`, { method: 'PUT', body: payload });
    else await api('/api/cronjobs', { method: 'POST', body: payload });
    setEditing(null);
    load();
  };
  const remove = async (id: string) => {
    if (!confirm('Delete cronjob?')) return;
    await api(`/api/cronjobs/${id}`, { method: 'DELETE' });
    load();
  };
  const apply = async (node_id: string) => {
    await api(`/api/nodes/${encodeURIComponent(node_id)}/cronjobs/apply`, { method: 'POST' });
    alert(`Applied to ${node_id}.`);
  };

  return (
    <div className="mx-auto max-w-6xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Cron jobs</h1>
          <p className="text-xs text-zinc-500">Written atomically to <code>/etc/cron.d/bpm</code> on each agent.</p>
        </div>
        <div className="flex gap-2">
          <select className="input w-44" value={filterNode} onChange={(e) => setFilterNode(e.target.value)}>
            <option value="">— all nodes —</option>
            {nodes.map((n) => <option key={n.id} value={n.id}>{n.hostname}</option>)}
          </select>
          <button onClick={() => setEditing({ enabled: true, run_as: 'root' })} className="btn-primary">New job</button>
        </div>
      </header>

      <div className="card overflow-hidden p-0">
        <table className="w-full text-sm">
          <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
            <tr>
              <th className="px-4 py-2">Node</th>
              <th className="px-4 py-2">Schedule</th>
              <th className="px-4 py-2">Command</th>
              <th className="px-4 py-2">User</th>
              <th className="px-4 py-2">Status</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {jobs.map((j) => (
              <tr key={j.id} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                <td className="px-4 py-2 font-mono text-[12px]">{j.node_id}</td>
                <td className="px-4 py-2 font-mono text-[12px]">{j.schedule}</td>
                <td className="px-4 py-2 font-mono text-[11px]">{j.command}</td>
                <td className="px-4 py-2 text-zinc-500">{j.run_as}</td>
                <td className="px-4 py-2"><span className={j.enabled ? 'badge-green' : 'badge-zinc'}>{j.enabled ? 'enabled' : 'disabled'}</span></td>
                <td className="px-4 py-2 text-right">
                  <button onClick={() => apply(j.node_id)} className="btn-ghost" title="re-apply to host">apply</button>
                  <button onClick={() => setEditing(j)} className="btn-ghost">edit</button>
                  <button onClick={() => remove(j.id)} className="btn-ghost text-red-600">delete</button>
                </td>
              </tr>
            ))}
            {jobs.length === 0 && <tr><td colSpan={6} className="px-4 py-6 text-center text-zinc-500">No jobs.</td></tr>}
          </tbody>
        </table>
      </div>

      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setEditing(null)}>
          <div className="card w-full max-w-lg" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-base font-semibold">{editing.id ? 'Edit job' : 'New job'}</h3>
            <div className="space-y-3 text-sm">
              <label className="block">
                <span className="text-xs text-zinc-500">Node</span>
                <select className="input mt-1" value={editing.node_id ?? ''} onChange={(e) => setEditing({ ...editing, node_id: e.target.value })}>
                  <option value="">— select node —</option>
                  {nodes.map((n) => <option key={n.id} value={n.id}>{n.hostname}</option>)}
                </select>
              </label>
              <div>
                <span className="text-xs text-zinc-500">Schedule</span>
                <div className="mt-1">
                  <SchedulePicker
                    variant="cron"
                    value={editing.schedule ?? '0 3 * * *'}
                    onChange={(v) => setEditing({ ...editing, schedule: v })}
                  />
                </div>
              </div>
              <label className="block">
                <span className="text-xs text-zinc-500">Command</span>
                <input className="input mt-1 font-mono text-[12px]" placeholder="echo hello >> /tmp/log" value={editing.command ?? ''} onChange={(e) => setEditing({ ...editing, command: e.target.value })} />
              </label>
              <div className="grid grid-cols-2 gap-3">
                <label className="block">
                  <span className="text-xs text-zinc-500">User</span>
                  <input className="input mt-1" value={editing.run_as ?? 'root'} onChange={(e) => setEditing({ ...editing, run_as: e.target.value })} />
                </label>
                <label className="block">
                  <span className="text-xs text-zinc-500">Comment</span>
                  <input className="input mt-1" value={editing.comment ?? ''} onChange={(e) => setEditing({ ...editing, comment: e.target.value })} />
                </label>
              </div>
              <label className="flex items-center gap-2">
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
