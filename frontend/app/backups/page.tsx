'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import { AuthShell } from '@/components/auth-shell';
import { fmtBytes, fmtRelative } from '@/lib/format';
import { SchedulePicker } from '@/components/schedule-picker';
import { Play, Pencil, Trash2 } from 'lucide-react';

interface BackupPlan {
  id: string; name: string; sources: string[];
  scope_type: string; scope_value: string;
  s3_endpoint_id: string; s3_bucket: string;
  key_template: string; schedule: string;
  retention_keep: number; compression: string; compression_level: number;
  webhook_id?: string; webhook_mode: string;
  enabled: boolean;
  created_at: number; updated_at: number;
  last_run_at?: number; next_run_at?: number;
}
interface BackupRun {
  id: string; plan_id: string; plan_name: string; triggered_by: string;
  started_at: number; finished_at?: number; status: string;
  error_summary?: string;
  items?: { node_id: string; status: string; bytes_uploaded: number; file_count: number; s3_key?: string; error?: string }[];
}
interface S3EndpointMini { id: string; name: string }
interface WebhookMini { id: string; name: string }

export default function BackupsPage() {
  return <AuthShell><Backups /></AuthShell>;
}

function Backups() {
  const [plans, setPlans] = useState<BackupPlan[]>([]);
  const [runs, setRuns] = useState<BackupRun[]>([]);
  const [endpoints, setEndpoints] = useState<S3EndpointMini[]>([]);
  const [webhooks, setWebhooks] = useState<WebhookMini[]>([]);
  const [editing, setEditing] = useState<Partial<BackupPlan> | null>(null);

  const load = async () => {
    try { setPlans((await api<{ plans: BackupPlan[] }>('/api/backups/plans')).plans ?? []); } catch { /* */ }
    try { setRuns((await api<{ runs: BackupRun[] }>('/api/backups/runs?limit=50')).runs ?? []); } catch { /* */ }
    try { setEndpoints(((await api<{ endpoints: S3EndpointMini[] }>('/api/s3/endpoints')).endpoints ?? []).map((e) => ({ id: e.id, name: e.name }))); } catch { /* */ }
    try { setWebhooks(((await api<{ webhooks: WebhookMini[] }>('/api/webhooks')).webhooks ?? []).map((w) => ({ id: w.id, name: w.name }))); } catch { /* */ }
  };
  useEffect(() => { load(); }, []);
  useSSE({
    'backup.started':  () => load(),
    'backup.finished': () => load(),
  });

  const save = async (p: Partial<BackupPlan>) => {
    const payload = {
      name: p.name ?? '', sources: p.sources ?? [],
      scope_type: p.scope_type ?? 'all', scope_value: p.scope_value ?? '',
      s3_endpoint_id: p.s3_endpoint_id ?? '', s3_bucket: p.s3_bucket ?? '',
      key_template: p.key_template ?? 'backups/{host}/{date}/{plan}.tar.gz',
      schedule: p.schedule ?? '0 0 3 * * *',
      retention_keep: p.retention_keep ?? 7,
      compression: p.compression ?? 'gzip', compression_level: p.compression_level ?? 6,
      webhook_id: p.webhook_id ?? '', webhook_mode: p.webhook_mode ?? 'on-error',
      enabled: p.enabled ?? true,
    };
    if (p.id) await api(`/api/backups/plans/${p.id}`, { method: 'PUT', body: payload });
    else await api('/api/backups/plans', { method: 'POST', body: payload });
    setEditing(null);
    load();
  };
  const remove = async (id: string) => {
    if (!confirm('Delete plan?')) return;
    await api(`/api/backups/plans/${id}`, { method: 'DELETE' });
    load();
  };
  const trigger = async (id: string) => {
    const r = await api<{ run_id: string }>(`/api/backups/plans/${id}/trigger`, { method: 'POST' });
    alert(`Run started: ${r.run_id}`);
    setTimeout(load, 1500);
  };

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Backups</h1>
          <p className="text-xs text-zinc-500">Agents stream tar.gz directly to S3 on schedule.</p>
        </div>
        <button onClick={() => setEditing({ enabled: true, scope_type: 'all', compression: 'gzip', compression_level: 6, retention_keep: 7, webhook_mode: 'on-error', schedule: '0 0 3 * * *', key_template: 'backups/{host}/{date}/{plan}.tar.gz' })} className="btn-primary">New plan</button>
      </header>

      <div className="card mb-6 overflow-hidden p-0">
        <table className="w-full text-sm">
          <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
            <tr>
              <th className="px-4 py-2">Name</th>
              <th className="px-4 py-2">Schedule</th>
              <th className="px-4 py-2">Scope</th>
              <th className="px-4 py-2">Bucket</th>
              <th className="px-4 py-2">Last run</th>
              <th className="px-4 py-2">Status</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {plans.map((p) => (
              <tr key={p.id} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                <td className="px-4 py-2 font-medium">{p.name}</td>
                <td className="px-4 py-2 font-mono text-[12px]">{p.schedule}</td>
                <td className="px-4 py-2 text-zinc-500">{p.scope_type === 'all' ? 'all' : `${p.scope_type}:${p.scope_value}`}</td>
                <td className="px-4 py-2 font-mono text-[11px]">{p.s3_bucket}</td>
                <td className="px-4 py-2 text-zinc-500">{p.last_run_at ? fmtRelative(p.last_run_at) : '—'}</td>
                <td className="px-4 py-2"><span className={p.enabled ? 'badge-green' : 'badge-zinc'}>{p.enabled ? 'enabled' : 'disabled'}</span></td>
                <td className="px-4 py-2 text-right">
                  <button onClick={() => trigger(p.id)} className="btn-primary mr-1 h-7 text-[12px]" title="Backup jetzt ausführen">
                    <Play className="h-3 w-3" /> backup now
                  </button>
                  <button onClick={() => setEditing(p)} className="btn-ghost"><Pencil className="h-3.5 w-3.5" /></button>
                  <button onClick={() => remove(p.id)} className="btn-ghost text-red-600"><Trash2 className="h-3.5 w-3.5" /></button>
                </td>
              </tr>
            ))}
            {plans.length === 0 && <tr><td colSpan={7} className="px-4 py-6 text-center text-zinc-500">No plans yet.</td></tr>}
          </tbody>
        </table>
      </div>

      <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-zinc-500">Recent runs</h2>
      <div className="card overflow-hidden p-0">
        <table className="w-full text-sm">
          <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
            <tr>
              <th className="px-4 py-2">When</th>
              <th className="px-4 py-2">Plan</th>
              <th className="px-4 py-2">By</th>
              <th className="px-4 py-2">Status</th>
              <th className="px-4 py-2">Nodes</th>
              <th className="px-4 py-2">Bytes</th>
            </tr>
          </thead>
          <tbody>
            {runs.map((r) => {
              const totalBytes = (r.items ?? []).reduce((a, b) => a + (b.bytes_uploaded || 0), 0);
              return (
                <tr key={r.id} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                  <td className="px-4 py-2 text-zinc-500">{fmtRelative(r.started_at)}</td>
                  <td className="px-4 py-2">{r.plan_name || r.plan_id}</td>
                  <td className="px-4 py-2 text-[11px] text-zinc-500">{r.triggered_by}</td>
                  <td className="px-4 py-2"><span className={r.status === 'success' ? 'badge-green' : r.status === 'partial' ? 'badge-amber' : r.status === 'failed' ? 'badge-red' : 'badge-zinc'}>{r.status}</span></td>
                  <td className="px-4 py-2 text-zinc-500">{r.items?.length ?? 0}</td>
                  <td className="px-4 py-2 font-mono text-[11px]">{fmtBytes(totalBytes)}</td>
                </tr>
              );
            })}
            {runs.length === 0 && <tr><td colSpan={6} className="px-4 py-6 text-center text-zinc-500">No runs yet.</td></tr>}
          </tbody>
        </table>
      </div>

      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setEditing(null)}>
          <div className="card w-full max-w-xl" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-base font-semibold">{editing.id ? 'Edit plan' : 'New plan'}</h3>
            <div className="space-y-3 text-sm">
              <Field label="Name"><input className="input" value={editing.name ?? ''} onChange={(e) => setEditing({ ...editing, name: e.target.value })} /></Field>
              <Field label="Sources (comma-separated absolute paths)">
                <input className="input font-mono text-[12px]" placeholder="/srv/data, /var/lib/docker/volumes/foo"
                  value={(editing.sources ?? []).join(', ')}
                  onChange={(e) => setEditing({ ...editing, sources: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) })} />
              </Field>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Scope">
                  <select className="input" value={editing.scope_type ?? 'all'} onChange={(e) => setEditing({ ...editing, scope_type: e.target.value })}>
                    <option value="all">all agents</option>
                    <option value="node">single node</option>
                    <option value="label">label k=v</option>
                  </select>
                </Field>
                <Field label="Scope value (host / label=value)">
                  <input className="input" value={editing.scope_value ?? ''} onChange={(e) => setEditing({ ...editing, scope_value: e.target.value })} />
                </Field>
              </div>
              <Field label="Schedule">
                <SchedulePicker
                  variant="seconds"
                  value={editing.schedule ?? '0 0 3 * * *'}
                  onChange={(v) => setEditing({ ...editing, schedule: v })}
                />
              </Field>
              <div className="grid grid-cols-2 gap-3">
                <Field label="S3 endpoint">
                  <select className="input" value={editing.s3_endpoint_id ?? ''} onChange={(e) => setEditing({ ...editing, s3_endpoint_id: e.target.value })}>
                    <option value="">— pick —</option>
                    {endpoints.map((ep) => <option key={ep.id} value={ep.id}>{ep.name}</option>)}
                  </select>
                </Field>
                <Field label="Bucket"><input className="input" value={editing.s3_bucket ?? ''} onChange={(e) => setEditing({ ...editing, s3_bucket: e.target.value })} /></Field>
              </div>
              <Field label="Key template"><input className="input font-mono text-[12px]" value={editing.key_template ?? ''} onChange={(e) => setEditing({ ...editing, key_template: e.target.value })} /></Field>
              <div className="grid grid-cols-3 gap-3">
                <Field label="Retention keep"><input type="number" className="input" value={editing.retention_keep ?? 7} onChange={(e) => setEditing({ ...editing, retention_keep: +e.target.value })} /></Field>
                <Field label="Compression">
                  <select className="input" value={editing.compression ?? 'gzip'} onChange={(e) => setEditing({ ...editing, compression: e.target.value })}>
                    <option value="gzip">gzip</option>
                    <option value="none">none</option>
                  </select>
                </Field>
                <Field label="Level"><input type="number" min={1} max={9} className="input" value={editing.compression_level ?? 6} onChange={(e) => setEditing({ ...editing, compression_level: +e.target.value })} /></Field>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Webhook">
                  <select className="input" value={editing.webhook_id ?? ''} onChange={(e) => setEditing({ ...editing, webhook_id: e.target.value })}>
                    <option value="">— none —</option>
                    {webhooks.map((w) => <option key={w.id} value={w.id}>{w.name}</option>)}
                  </select>
                </Field>
                <Field label="Webhook mode">
                  <select className="input" value={editing.webhook_mode ?? 'on-error'} onChange={(e) => setEditing({ ...editing, webhook_mode: e.target.value })}>
                    <option value="always">always</option>
                    <option value="on-error">on-error</option>
                    <option value="off">off</option>
                  </select>
                </Field>
              </div>
              <label className="flex items-center gap-2"><input type="checkbox" checked={editing.enabled ?? true} onChange={(e) => setEditing({ ...editing, enabled: e.target.checked })} /> Enabled</label>
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

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="text-xs text-zinc-500">{label}</span>
      <div className="mt-1">{children}</div>
    </label>
  );
}
