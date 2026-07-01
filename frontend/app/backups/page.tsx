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
  engine?: string;              // tar | pgdump
  verify_restore?: boolean;     // pgdump only
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
interface NodeMini { id: string; hostname: string; labels?: Record<string, string> }

export default function BackupsPage() {
  return <AuthShell><Backups /></AuthShell>;
}

function Backups() {
  const [plans, setPlans] = useState<BackupPlan[]>([]);
  const [runs, setRuns] = useState<BackupRun[]>([]);
  const [endpoints, setEndpoints] = useState<S3EndpointMini[]>([]);
  const [webhooks, setWebhooks] = useState<WebhookMini[]>([]);
  const [nodes, setNodes] = useState<NodeMini[]>([]);
  const [editing, setEditing] = useState<Partial<BackupPlan> | null>(null);

  const load = async () => {
    try { setPlans((await api<{ plans: BackupPlan[] }>('/api/backups/plans')).plans ?? []); } catch { /* */ }
    try { setRuns((await api<{ runs: BackupRun[] }>('/api/backups/runs?limit=50')).runs ?? []); } catch { /* */ }
    try { setEndpoints(((await api<{ endpoints: S3EndpointMini[] }>('/api/s3/endpoints')).endpoints ?? []).map((e) => ({ id: e.id, name: e.name }))); } catch { /* */ }
    try { setWebhooks(((await api<{ webhooks: WebhookMini[] }>('/api/webhooks')).webhooks ?? []).map((w) => ({ id: w.id, name: w.name }))); } catch { /* */ }
    try { setNodes(((await api<{ nodes: NodeMini[] }>('/api/nodes')).nodes ?? []).map((n) => ({ id: n.id, hostname: n.hostname, labels: n.labels }))); } catch { /* */ }
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
      engine: p.engine ?? 'tar', verify_restore: p.verify_restore ?? false,
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
  const deleteRun = async (id: string) => {
    if (!confirm('Delete this backup run record? The S3 objects are not touched.')) return;
    await api(`/api/backups/runs/${id}`, { method: 'DELETE' });
    load();
  };
  const failedCount = runs.filter((r) => r.status === 'failed').length;
  const cleanupFailed = async () => {
    if (!confirm(`Delete all ${failedCount} failed run record(s)? The S3 objects are not touched.`)) return;
    await api('/api/backups/runs/cleanup-failed', { method: 'POST' });
    load();
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

      <div className="mb-2 flex items-center justify-between">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-zinc-500">Recent runs</h2>
        {failedCount > 0 && (
          <button onClick={cleanupFailed} className="btn-ghost h-7 text-[12px] text-red-600" title="Delete all failed run records at once">
            <Trash2 className="h-3.5 w-3.5" /> cleanup failed ({failedCount})
          </button>
        )}
      </div>
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
              <th className="px-4 py-2"></th>
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
                  <td className="px-4 py-2"><span title={r.error_summary || undefined} className={r.status === 'success' ? 'badge-green' : r.status === 'partial' ? 'badge-amber' : r.status === 'failed' ? 'badge-red' : 'badge-zinc'}>{r.status}</span></td>
                  <td className="px-4 py-2 text-zinc-500">{r.items?.length ?? 0}</td>
                  <td className="px-4 py-2 font-mono text-[11px]">{fmtBytes(totalBytes)}</td>
                  <td className="px-4 py-2 text-right">
                    {(r.status === 'failed' || r.status === 'partial' || r.status === 'cancelled') && (
                      <button onClick={() => deleteRun(r.id)} className="btn-ghost h-7 text-red-600" title="Delete this run record">
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </td>
                </tr>
              );
            })}
            {runs.length === 0 && <tr><td colSpan={7} className="px-4 py-6 text-center text-zinc-500">No runs yet.</td></tr>}
          </tbody>
        </table>
      </div>

      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setEditing(null)}>
          <div className="card w-full max-w-xl" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-base font-semibold">{editing.id ? 'Edit plan' : 'New plan'}</h3>
            <div className="space-y-3 text-sm">
              <Field label="Name"><input className="input" value={editing.name ?? ''} onChange={(e) => setEditing({ ...editing, name: e.target.value })} /></Field>

              <Field label="Engine">
                <div className="inline-flex rounded-md border border-zinc-200 p-0.5 text-xs dark:border-zinc-700">
                  {[
                    { v: 'tar',    label: 'tar+gzip',     desc: 'Volume snapshot (existing behaviour)' },
                    { v: 'pgdump', label: 'pg_dump',      desc: 'Postgres logical dump → optional verify in sidecar' },
                  ].map((opt) => (
                    <button key={opt.v} type="button" title={opt.desc}
                      onClick={() => setEditing({ ...editing, engine: opt.v })}
                      className={`rounded px-3 py-1 ${(editing.engine ?? 'tar') === opt.v ? 'bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900' : 'text-zinc-600 dark:text-zinc-400'}`}
                    >{opt.label}</button>
                  ))}
                </div>
                {(editing.engine ?? 'tar') === 'pgdump' && (
                  <label className="mt-2 flex cursor-pointer items-center gap-2 text-xs text-zinc-600 dark:text-zinc-400">
                    <input type="checkbox"
                      checked={editing.verify_restore ?? false}
                      onChange={(e) => setEditing({ ...editing, verify_restore: e.target.checked })} />
                    Verify by sidecar-restore (spawns throwaway postgres:15-alpine, pg_restore, +30-60s but guarantees the dump is restorable)
                  </label>
                )}
              </Field>

              {(editing.engine ?? 'tar') === 'pgdump' ? (
                <Field label="Postgres DSNs — one per line">
                  <textarea
                    rows={3}
                    className="input font-mono text-[11px]"
                    placeholder="host=db user=supabase_admin dbname=postgres password_secret=supabase-postgres-password"
                    value={(editing.sources ?? []).join('\n')}
                    onChange={(e) => setEditing({ ...editing, sources: e.target.value.split('\n').map((s) => s.trim()).filter(Boolean) })}
                  />
                  <p className="mt-1 text-[11px] text-zinc-500">
                    libpq <code>key=value</code> format. <code>password_secret=</code> reads
                    <code> /run/secrets/&lt;name&gt;</code> on the agent — the password never lives in the plan row.
                    One line = one database = one .dump file in S3.
                  </p>
                </Field>
              ) : (
                <Field label="Sources (comma-separated absolute paths)">
                  <input className="input font-mono text-[12px]" placeholder="/srv/data, /var/lib/docker/volumes/foo"
                    value={(editing.sources ?? []).join(', ')}
                    onChange={(e) => setEditing({ ...editing, sources: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) })} />
                </Field>
              )}
              <Field label="Scope">
                <ScopePicker
                  type={editing.scope_type ?? 'all'}
                  value={editing.scope_value ?? ''}
                  nodes={nodes}
                  onChange={(t, v) => setEditing({ ...editing, scope_type: t, scope_value: v })}
                />
              </Field>
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

// ScopePicker — three modes:
//   all   : every connected agent (no value)
//   node  : multiselect of currently-known agents (CSV of hostnames)
//   label : free text in the form key=value (with autocomplete hint
//           pulled from labels seen across the fleet)
function ScopePicker({ type, value, nodes, onChange }: {
  type: string;
  value: string;
  nodes: NodeMini[];
  onChange: (type: string, value: string) => void;
}) {
  const selected = new Set(value.split(',').map((s) => s.trim()).filter(Boolean));
  const toggle = (host: string) => {
    if (selected.has(host)) selected.delete(host);
    else selected.add(host);
    onChange('node', Array.from(selected).join(','));
  };

  // Collect distinct labels for the autocomplete hint.
  const labelHints = new Set<string>();
  for (const n of nodes) {
    for (const [k, v] of Object.entries(n.labels ?? {})) {
      labelHints.add(`${k}=${v}`);
    }
  }

  return (
    <div className="space-y-2">
      <div className="inline-flex rounded-md border border-zinc-200 p-0.5 text-xs dark:border-zinc-700">
        {[
          { v: 'all',   label: 'All agents' },
          { v: 'node',  label: 'Pick nodes' },
          { v: 'label', label: 'By label' },
        ].map((opt) => (
          <button key={opt.v} type="button"
            onClick={() => onChange(opt.v, opt.v === 'all' ? '' : value)}
            className={`rounded px-2 py-1 ${type === opt.v ? 'bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900' : 'text-zinc-600 dark:text-zinc-400'}`}
          >{opt.label}</button>
        ))}
      </div>

      {type === 'all' && (
        <p className="text-[11px] text-zinc-500">
          Runs on every currently-connected agent ({nodes.length} {nodes.length === 1 ? 'node' : 'nodes'} right now).
        </p>
      )}

      {type === 'node' && (
        <div className="space-y-1.5 rounded-md bg-zinc-50 p-2 dark:bg-zinc-900/40">
          {nodes.length === 0 ? (
            <p className="text-[11px] text-zinc-500">No agents are connected right now. The plan will run on whatever's online when it fires.</p>
          ) : nodes.map((n) => (
            <label key={n.id} className="flex cursor-pointer items-center gap-2 rounded px-1 py-0.5 text-xs hover:bg-zinc-100 dark:hover:bg-zinc-800">
              <input
                type="checkbox"
                checked={selected.has(n.hostname)}
                onChange={() => toggle(n.hostname)}
              />
              <span className="font-mono">{n.hostname}</span>
              {n.labels && Object.keys(n.labels).length > 0 && (
                <span className="text-[10px] text-zinc-500">
                  {Object.entries(n.labels).map(([k, v]) => `${k}=${v}`).join(' · ')}
                </span>
              )}
            </label>
          ))}
          <p className="mt-2 text-[10px] text-zinc-500">{selected.size} selected</p>
        </div>
      )}

      {type === 'label' && (
        <div>
          <input
            className="input font-mono text-[12px]"
            placeholder="role=worker"
            value={value}
            onChange={(e) => onChange('label', e.target.value)}
          />
          <p className="mt-1 text-[11px] text-zinc-500">
            Format: <code>key=value</code> — matches any agent whose labels include the pair.
            {labelHints.size > 0 && (
              <> Examples from your fleet: {Array.from(labelHints).slice(0, 5).map((s, i) => (
                <button key={i} type="button" onClick={() => onChange('label', s)}
                  className="ml-1 rounded bg-zinc-100 px-1 font-mono text-[10px] hover:bg-zinc-200 dark:bg-zinc-800 dark:hover:bg-zinc-700">{s}</button>
              ))}</>
            )}
            <br />
            Agent labels are set per-agent via the <code>BPM_AGENT_LABELS</code> env var
            (e.g. <code>role=worker,zone=eu</code>) in your compose file.
          </p>
        </div>
      )}
    </div>
  );
}
