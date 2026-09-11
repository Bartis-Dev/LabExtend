'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { AuthShell } from '@/components/auth-shell';
import type { NodeView } from '@/lib/types';
import { SchedulePicker } from '@/components/schedule-picker';
import { Toast } from '@/components/toast';

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

/** Create, update and delete answer with whether the node actually took the change. */
interface ApplyResult {
  applied?: boolean;
  apply_error?: string;
}

const errMsg = (e: unknown) => (e as { body?: { error?: string } })?.body?.error ?? String(e);

export default function CronJobsPage() {
  return <AuthShell><Cron /></AuthShell>;
}

function Cron() {
  const [nodes, setNodes] = useState<NodeView[]>([]);
  const [jobs, setJobs] = useState<CronJob[]>([]);
  const [editing, setEditing] = useState<Partial<CronJob> | null>(null);
  const [filterNode, setFilterNode] = useState('');
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [formErr, setFormErr] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<{ message: string; tone: 'success' | 'error' } | null>(null);

  const load = async () => {
    try {
      const n = await api<{ nodes: NodeView[] }>('/api/nodes');
      setNodes(n.nodes ?? []);
      const url = filterNode ? `/api/nodes/${encodeURIComponent(filterNode)}/cronjobs` : '/api/cronjobs';
      const j = await api<{ jobs: CronJob[] }>(url);
      setJobs(j.jobs ?? []);
      setLoadErr(null);
    } catch (e) {
      setLoadErr(errMsg(e));
    }
  };
  useEffect(() => { load(); }, [filterNode]);

  const openEditor = (j: Partial<CronJob>) => {
    setFormErr(null);
    setEditing(j);
  };

  // The job is stored even when its node could not take it, so "saved" alone
  // would claim something the host is not doing.
  const reportApply = (r: ApplyResult, node: string, done: string) => {
    if (r.applied === false) {
      setToast({ tone: 'error', message: `${done}, but not applied to ${node}: ${r.apply_error ?? 'unknown error'}` });
    } else {
      setToast({ tone: 'success', message: `${done} and applied to ${node}.` });
    }
  };

  const save = async (j: Partial<CronJob>) => {
    const payload = {
      node_id: j.node_id ?? '', schedule: j.schedule ?? '', command: j.command ?? '',
      run_as: j.run_as || 'root', comment: j.comment ?? '', enabled: j.enabled ?? true,
    };
    setSaving(true);
    setFormErr(null);
    try {
      const r = j.id
        ? await api<ApplyResult>(`/api/cronjobs/${j.id}`, { method: 'PUT', body: payload })
        : await api<ApplyResult>('/api/cronjobs', { method: 'POST', body: payload });
      setEditing(null);
      reportApply(r, payload.node_id, 'Saved');
      load();
    } catch (e) {
      setFormErr(errMsg(e));
    } finally {
      setSaving(false);
    }
  };
  const remove = async (j: CronJob) => {
    if (!confirm('Delete cronjob?')) return;
    try {
      const r = await api<ApplyResult>(`/api/cronjobs/${j.id}`, { method: 'DELETE' });
      reportApply(r, j.node_id, 'Deleted');
      load();
    } catch (e) {
      setToast({ tone: 'error', message: errMsg(e) });
    }
  };
  const apply = async (node_id: string) => {
    try {
      await api(`/api/nodes/${encodeURIComponent(node_id)}/cronjobs/apply`, { method: 'POST' });
      setToast({ tone: 'success', message: `Applied to ${node_id}.` });
    } catch (e) {
      setToast({ tone: 'error', message: `Not applied to ${node_id}: ${errMsg(e)}` });
    }
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
          <button onClick={() => openEditor({ enabled: true, run_as: 'root' })} className="btn-primary">New job</button>
        </div>
      </header>

      {loadErr && (
        <p className="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
          {loadErr}
        </p>
      )}

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
                  <button onClick={() => openEditor(j)} className="btn-ghost">edit</button>
                  <button onClick={() => remove(j)} className="btn-ghost text-red-600">delete</button>
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
            {formErr && (
              <p className="mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
                {formErr}
              </p>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <button onClick={() => setEditing(null)} className="btn-ghost">Cancel</button>
              <button onClick={() => save(editing)} disabled={saving} className="btn-primary">
                {saving ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {toast && (
        <Toast
          message={toast.message}
          tone={toast.tone}
          timeoutMs={toast.tone === 'error' ? 10_000 : 4000}
          onDone={() => setToast(null)}
        />
      )}
    </div>
  );
}
