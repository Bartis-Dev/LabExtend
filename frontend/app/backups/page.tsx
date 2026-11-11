'use client';

import { Suspense, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { api, errText } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import { AuthShell } from '@/components/auth-shell';
import { fmtBytes, fmtRelative } from '@/lib/format';
import { SchedulePicker } from '@/components/schedule-picker';
import { Database, Play, Pencil, Trash2, History } from 'lucide-react';
import { Modal } from '@/components/modal';
import { Field, inputCls } from '@/components/field';
import { confirmDialog } from '@/components/confirm';
import { toast } from '@/components/toast';
import { EmptyState, ErrorState, TableSkeleton } from '@/components/feedback';
import { Tooltip } from '@/components/tooltip';

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
  return (
    <AuthShell>
      <Suspense fallback={<div className="page"><TableSkeleton rows={3} cols={6} /></div>}>
        <Backups />
      </Suspense>
    </AuthShell>
  );
}

function Backups() {
  const [plans, setPlans] = useState<BackupPlan[]>([]);
  const [runs, setRuns] = useState<BackupRun[]>([]);
  const [endpoints, setEndpoints] = useState<S3EndpointMini[]>([]);
  const [webhooks, setWebhooks] = useState<WebhookMini[]>([]);
  const [nodes, setNodes] = useState<NodeMini[]>([]);
  const [editing, setEditing] = useState<Partial<BackupPlan> | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [formErr, setFormErr] = useState<string | null>(null);
  const [fieldErr, setFieldErr] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  // Which list failed, so its empty state ("No plans yet") is not shown next
  // to the load error as if the list were really empty.
  const [failedList, setFailedList] = useState({ plans: false, runs: false });
  // Plan ids whose "Back up now" request is in flight.
  const [triggering, setTriggering] = useState<Set<string>>(new Set());

  const load = async () => {
    // Plans and runs are the page; the lookups below only feed the editor, so
    // their failures stay quiet and just leave a dropdown empty.
    let failed: string | null = null;
    const bad = { plans: false, runs: false };
    try { setPlans((await api<{ plans: BackupPlan[] }>('/api/backups/plans')).plans ?? []); } catch (e) { failed = errText(e, 'Could not load backup plans.'); bad.plans = true; }
    try { setRuns((await api<{ runs: BackupRun[] }>('/api/backups/runs?limit=50')).runs ?? []); } catch (e) { failed ??= errText(e, 'Could not load backup runs.'); bad.runs = true; }
    setLoadErr(failed);
    setFailedList(bad);
    setLoaded(true);
    try { setEndpoints(((await api<{ endpoints: S3EndpointMini[] }>('/api/s3/endpoints')).endpoints ?? []).map((e) => ({ id: e.id, name: e.name }))); } catch { /* */ }
    try { setWebhooks(((await api<{ webhooks: WebhookMini[] }>('/api/webhooks')).webhooks ?? []).map((w) => ({ id: w.id, name: w.name }))); } catch { /* */ }
    try { setNodes(((await api<{ nodes: NodeMini[] }>('/api/nodes')).nodes ?? []).map((n) => ({ id: n.id, hostname: n.hostname, labels: n.labels }))); } catch { /* */ }
  };
  useEffect(() => { load(); }, []);
  // Reactive, so the bell's "Open" link re-highlights when already on /backups.
  const focusRun = useSearchParams().get('run');
  useEffect(() => {
    if (focusRun && runs.some((r) => r.id === focusRun)) {
      document.getElementById(`run-${focusRun}`)?.scrollIntoView({ block: 'center' });
    }
  }, [focusRun, runs]);
  useSSE({
    'backup.started':  () => load(),
    'backup.finished': () => load(),
  });

  const openEditor = (p: Partial<BackupPlan>) => { setFormErr(null); setFieldErr({}); setEditing(p); };

  const save = async (p: Partial<BackupPlan>) => {
    const engine = p.engine ?? 'tar';
    const errs: Record<string, string> = {};
    if (!p.name?.trim()) errs.name = 'Give the plan a name.';
    if (!(p.sources ?? []).length) errs.sources = engine === 'pgdump' ? 'Add at least one DSN.' : 'Add at least one absolute path.';
    else if (engine === 'tar' && (p.sources ?? []).some((x) => !x.startsWith('/'))) errs.sources = 'Paths must be absolute (start with /).';
    if (!p.s3_endpoint_id) errs.s3_endpoint_id = 'Pick an S3 endpoint.';
    if (!p.s3_bucket?.trim()) errs.s3_bucket = 'Enter the bucket name.';
    if ((p.scope_type ?? 'all') !== 'all' && !p.scope_value?.trim()) errs.scope = p.scope_type === 'node' ? 'Pick at least one node.' : 'Enter a key=value label.';
    if ((p.retention_keep ?? 7) < 0) errs.retention_keep = 'Use 0 or more.';
    setFieldErr(errs);
    if (Object.keys(errs).length) return;
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
    setSaving(true);
    setFormErr(null);
    try {
      if (p.id) await api(`/api/backups/plans/${p.id}`, { method: 'PUT', body: payload });
      else await api('/api/backups/plans', { method: 'POST', body: payload });
      setEditing(null);
      toast.success(`Plan ${payload.name} saved.`);
      load();
    } catch (e) {
      setFormErr(errText(e, 'Saving failed.'));
    } finally {
      setSaving(false);
    }
  };
  const remove = async (p: BackupPlan) => {
    const ok = await confirmDialog({
      title: `Delete plan ${p.name}?`,
      body: 'The schedule stops. Backups already in the bucket stay there.',
      confirmLabel: 'Delete plan',
      danger: true,
    });
    if (!ok) return;
    try {
      await api(`/api/backups/plans/${p.id}`, { method: 'DELETE' });
      toast.success(`Plan ${p.name} deleted.`);
      load();
    } catch (e) {
      toast.error('Delete failed: ' + errText(e, 'no answer from the leader'));
    }
  };
  const trigger = async (p: BackupPlan) => {
    if (triggering.has(p.id)) return;
    setTriggering((s) => new Set(s).add(p.id));
    try {
      await api<{ run_id: string }>(`/api/backups/plans/${p.id}/trigger`, { method: 'POST' });
      toast.success(`Backup of ${p.name} started. It shows up under Recent runs.`);
      setTimeout(load, 1500);
    } catch (e) {
      toast.error('Could not start the backup: ' + errText(e, 'no answer from the leader'));
    } finally {
      setTriggering((s) => { const n = new Set(s); n.delete(p.id); return n; });
    }
  };
  const deleteRun = async (id: string) => {
    const ok = await confirmDialog({
      title: 'Delete this run record?',
      body: 'Only the record in LabExtend is removed. The S3 objects are not touched.',
      confirmLabel: 'Delete record',
      danger: true,
    });
    if (!ok) return;
    try {
      await api(`/api/backups/runs/${id}`, { method: 'DELETE' });
      load();
    } catch (e) {
      toast.error('Delete failed: ' + errText(e, 'no answer from the leader'));
    }
  };
  const failedCount = runs.filter((r) => r.status === 'failed').length;
  const cleanupFailed = async () => {
    const ok = await confirmDialog({
      title: `Delete ${failedCount} failed run record${failedCount === 1 ? '' : 's'}?`,
      body: 'Only the records in LabExtend are removed. The S3 objects are not touched.',
      confirmLabel: 'Delete records',
      danger: true,
    });
    if (!ok) return;
    try {
      await api('/api/backups/runs/cleanup-failed', { method: 'POST' });
      toast.success('Failed run records deleted.');
      load();
    } catch (e) {
      toast.error('Cleanup failed: ' + errText(e, 'no answer from the leader'));
    }
  };
  const newPlan = () => openEditor({ enabled: true, scope_type: 'all', compression: 'gzip', compression_level: 6, retention_keep: 7, webhook_mode: 'on-error', schedule: '0 0 3 * * *', key_template: 'backups/{host}/{date}/{plan}.tar.gz' });
  const engine = editing?.engine ?? 'tar';

  return (
    <div className="page">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Backups</h1>
          <p className="page-sub">Agents stream tar.gz directly to S3 on schedule.</p>
        </div>
        <button onClick={newPlan} className="btn-primary">New plan</button>
      </header>

      {loadErr && <ErrorState message={loadErr} onRetry={load} />}

      {!loaded ? <TableSkeleton rows={3} cols={6} /> : (<>
      <div className="card mb-8 overflow-x-auto p-0">
        <table className="w-full min-w-[48rem] text-sm">
          <thead className="border-b border-zinc-200 dark:border-zinc-800">
            <tr>
              <th className="th">Name</th>
              <th className="th">Schedule</th>
              <th className="th">Scope</th>
              <th className="th">Bucket</th>
              <th className="th">Last run</th>
              <th className="th">Status</th>
              <th className="th"></th>
            </tr>
          </thead>
          <tbody>
            {plans.map((p) => (
              <tr key={p.id} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                <td className="td whitespace-nowrap font-medium">{p.name}</td>
                <td className="td whitespace-nowrap font-mono text-xs">{p.schedule}</td>
                <td className="td whitespace-nowrap text-zinc-500">{p.scope_type === 'all' ? 'all' : `${p.scope_type}:${p.scope_value}`}</td>
                <td className="td whitespace-nowrap font-mono text-2xs">{p.s3_bucket}</td>
                <td className="td text-zinc-500">{p.last_run_at ? fmtRelative(p.last_run_at) : '-'}</td>
                <td className="td"><span className={p.enabled ? 'badge-green' : 'badge-zinc'}>{p.enabled ? 'enabled' : 'disabled'}</span></td>
                <td className="td text-right">
                  <div className="inline-flex items-center gap-1">
                    <button onClick={() => trigger(p)} disabled={triggering.has(p.id)} className="btn-primary btn-sm">
                      <Play className="h-3.5 w-3.5" /> {triggering.has(p.id) ? 'Starting…' : 'Back up now'}
                    </button>
                    <Tooltip label="Edit plan">
                      <button onClick={() => openEditor(p)} className="btn-icon" aria-label={`Edit ${p.name}`}><Pencil className="h-4 w-4" /></button>
                    </Tooltip>
                    <Tooltip label="Delete plan">
                      <button onClick={() => remove(p)} className="btn-icon text-red-600 hover:text-red-700 dark:text-red-400" aria-label={`Delete ${p.name}`}><Trash2 className="h-4 w-4" /></button>
                    </Tooltip>
                  </div>
                </td>
              </tr>
            ))}
            {plans.length === 0 && !failedList.plans && (
              <tr><td colSpan={7}>
                <EmptyState
                  icon={<Database className="h-6 w-6" />}
                  title="No backup plans yet"
                  action={<button onClick={newPlan} className="btn-primary">New plan</button>}
                >
                  A plan picks paths or Postgres databases, a schedule and an S3 bucket. Add an S3 endpoint first if you have none.
                </EmptyState>
              </td></tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="mb-3 flex items-center justify-between">
        <h2 className="section-title">Recent runs</h2>
        {failedCount > 0 && (
          <button onClick={cleanupFailed} className="btn-ghost-danger btn-sm">
            <Trash2 className="h-3.5 w-3.5" /> Delete failed runs ({failedCount})
          </button>
        )}
      </div>
      <div className="card overflow-x-auto p-0">
        <table className="w-full min-w-[48rem] text-sm">
          <thead className="border-b border-zinc-200 dark:border-zinc-800">
            <tr>
              <th className="th">When</th>
              <th className="th">Plan</th>
              <th className="th">By</th>
              <th className="th">Status</th>
              <th className="th">Nodes</th>
              <th className="th">Bytes</th>
              <th className="th"></th>
            </tr>
          </thead>
          <tbody>
            {runs.map((r) => {
              const totalBytes = (r.items ?? []).reduce((a, b) => a + (b.bytes_uploaded || 0), 0);
              return (
                <tr key={r.id} id={`run-${r.id}`} className={`border-b border-zinc-100 last:border-0 dark:border-zinc-800 ${r.id === focusRun ? 'bg-accent/5 ring-1 ring-inset ring-accent/40' : ''}`}>
                  <td className="td text-zinc-500">{fmtRelative(r.started_at)}</td>
                  <td className="td">{r.plan_name || r.plan_id}</td>
                  <td className="td text-2xs text-zinc-500">{r.triggered_by}</td>
                  <td className="td">{(() => {
                    const badge = <span className={r.status === 'success' ? 'badge-green' : r.status === 'partial' ? 'badge-amber' : r.status === 'failed' ? 'badge-red' : 'badge-zinc'}>{r.status}</span>;
                    return r.error_summary && r.id !== focusRun
                      ? <Tooltip label={<span className="whitespace-pre-line break-words">{r.error_summary}</span>}>{badge}</Tooltip>
                      : badge;
                  })()}
                    {r.id === focusRun && r.error_summary && <div className="mt-1 max-w-md whitespace-pre-line break-words text-2xs text-red-600 dark:text-red-400">{r.error_summary}</div>}</td>
                  <td className="td text-zinc-500">{r.items?.length ?? 0}</td>
                  <td className="td font-mono text-2xs">{fmtBytes(totalBytes)}</td>
                  <td className="td text-right">
                    {(r.status === 'failed' || r.status === 'partial' || r.status === 'cancelled') && (
                      <Tooltip label="Delete this run record">
                        <button onClick={() => deleteRun(r.id)} className="btn-icon text-red-600 hover:text-red-700 dark:text-red-400" aria-label="Delete this run record">
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </Tooltip>
                    )}
                  </td>
                </tr>
              );
            })}
            {runs.length === 0 && !failedList.runs && (
              <tr><td colSpan={7}>
                <EmptyState icon={<History className="h-6 w-6" />} title="No runs yet">
                  Runs appear here when a plan fires on schedule or you start one with Back up now.
                </EmptyState>
              </td></tr>
            )}
          </tbody>
        </table>
      </div>
      </>)}

      {editing && (
        <Modal
          title={editing.id ? 'Edit plan' : 'New plan'}
          width="max-w-2xl"
          onClose={() => setEditing(null)}
          dismissable={!saving}
          footer={
            <>
              <button type="button" onClick={() => setEditing(null)} disabled={saving} className="btn-ghost">Cancel</button>
              <button type="submit" form="plan-form" disabled={saving} className="btn-primary">{saving ? 'Saving…' : 'Save'}</button>
            </>
          }
        >
          <form id="plan-form" noValidate onSubmit={(e) => { e.preventDefault(); save(editing); }} className="space-y-4 text-sm">
            <Field label="Name" error={fieldErr.name}>
              <input className={inputCls(fieldErr.name)} value={editing.name ?? ''} onChange={(e) => setEditing({ ...editing, name: e.target.value })} />
            </Field>

            <Field label="Engine" group hint={ENGINES.find((x) => x.v === engine)?.desc}>
              <div className="seg" role="radiogroup" aria-label="Engine">
                {ENGINES.map((opt) => (
                  <button key={opt.v} type="button" role="radio" aria-checked={engine === opt.v}
                    onClick={() => setEditing({ ...editing, engine: opt.v })}
                    className={engine === opt.v ? 'seg-item-active' : 'seg-item'}
                  >{opt.label}</button>
                ))}
              </div>
              {engine === 'pgdump' && (
                <label className="mt-3 flex cursor-pointer items-start gap-2 text-xs text-zinc-600 dark:text-zinc-400">
                  <input type="checkbox" className="mt-px"
                    checked={editing.verify_restore ?? false}
                    onChange={(e) => setEditing({ ...editing, verify_restore: e.target.checked })} />
                  Verify by sidecar restore (spawns a throwaway postgres:15-alpine and runs pg_restore; adds 30 to 60s but proves the dump is restorable)
                </label>
              )}
            </Field>

            {engine === 'pgdump' ? (
              <Field
                label="Postgres DSNs, one per line"
                error={fieldErr.sources}
                hint={<>libpq <code>key=value</code> format. <code>password_secret=</code> reads <code>/run/secrets/&lt;name&gt;</code> on the agent, so the password never lives in the plan row. One line is one database is one .dump file in S3.</>}
              >
                <textarea
                  rows={3}
                  className={inputCls(fieldErr.sources, 'font-mono text-xs')}
                  placeholder="host=db user=supabase_admin dbname=postgres password_secret=supabase-postgres-password"
                  value={(editing.sources ?? []).join('\n')}
                  onChange={(e) => setEditing({ ...editing, sources: e.target.value.split('\n').map((x) => x.trim()).filter(Boolean) })}
                />
              </Field>
            ) : (
              <Field label="Sources (comma-separated absolute paths)" error={fieldErr.sources}>
                <input className={inputCls(fieldErr.sources, 'font-mono text-xs')} placeholder="/srv/data, /var/lib/docker/volumes/foo"
                  value={(editing.sources ?? []).join(', ')}
                  onChange={(e) => setEditing({ ...editing, sources: e.target.value.split(',').map((x) => x.trim()).filter(Boolean) })} />
              </Field>
            )}
            <Field label="Scope" group error={fieldErr.scope}>
              <ScopePicker
                type={editing.scope_type ?? 'all'}
                value={editing.scope_value ?? ''}
                nodes={nodes}
                onChange={(t, v) => setEditing({ ...editing, scope_type: t, scope_value: v })}
              />
            </Field>
            <Field label="Schedule" group>
              <SchedulePicker
                variant="seconds"
                value={editing.schedule ?? '0 0 3 * * *'}
                onChange={(v) => setEditing({ ...editing, schedule: v })}
              />
            </Field>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field label="S3 endpoint" error={fieldErr.s3_endpoint_id} hint={endpoints.length === 0 ? 'No endpoints yet. Add one on the S3 page.' : undefined}>
                <select className={inputCls(fieldErr.s3_endpoint_id)} value={editing.s3_endpoint_id ?? ''} onChange={(e) => setEditing({ ...editing, s3_endpoint_id: e.target.value })}>
                  <option value="">Pick an endpoint</option>
                  {endpoints.map((ep) => <option key={ep.id} value={ep.id}>{ep.name}</option>)}
                </select>
              </Field>
              <Field label="Bucket" error={fieldErr.s3_bucket}>
                <input className={inputCls(fieldErr.s3_bucket)} value={editing.s3_bucket ?? ''} onChange={(e) => setEditing({ ...editing, s3_bucket: e.target.value })} />
              </Field>
            </div>
            <Field label="Key template">
              <input className="input font-mono text-xs" value={editing.key_template ?? ''} onChange={(e) => setEditing({ ...editing, key_template: e.target.value })} />
            </Field>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
              <Field label="Retention keep" error={fieldErr.retention_keep} hint="Successful backups kept per node; older ones are deleted from the bucket after each run. 0 keeps everything.">
                <input type="number" min={0} className={inputCls(fieldErr.retention_keep)} value={editing.retention_keep ?? 7} onChange={(e) => setEditing({ ...editing, retention_keep: +e.target.value })} />
              </Field>
              <Field label="Compression">
                <select className="input" value={editing.compression ?? 'gzip'} onChange={(e) => setEditing({ ...editing, compression: e.target.value })}>
                  <option value="gzip">gzip</option>
                  <option value="none">none</option>
                </select>
              </Field>
              <Field label="Level">
                <input type="number" min={1} max={9} className="input" value={editing.compression_level ?? 6} onChange={(e) => setEditing({ ...editing, compression_level: Math.min(9, Math.max(1, +e.target.value || 6)) })} />
              </Field>
            </div>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field label="Success webhook">
                <select className="input" value={editing.webhook_id ?? ''} onChange={(e) => setEditing({ ...editing, webhook_id: e.target.value })}>
                  <option value="">None</option>
                  {webhooks.map((w) => <option key={w.id} value={w.id}>{w.name}</option>)}
                </select>
              </Field>
              <Field label="Successful runs">
                <select className="input" value={editing.webhook_mode === 'always' ? 'always' : 'on-error'} onChange={(e) => setEditing({ ...editing, webhook_mode: e.target.value })}>
                  <option value="on-error">not posted</option>
                  <option value="always">posted to this webhook</option>
                </select>
              </Field>
            </div>
            <p className="hint">Failed and partial runs always raise a notification and go to every webhook subscribed to backup.failed (see Webhooks).</p>
            <label className="flex cursor-pointer items-center gap-2"><input type="checkbox" checked={editing.enabled ?? true} onChange={(e) => setEditing({ ...editing, enabled: e.target.checked })} /> Enabled</label>
            {formErr && <p role="alert" className="alert-error">{formErr}</p>}
          </form>
        </Modal>
      )}
    </div>
  );
}

const ENGINES = [
  { v: 'tar', label: 'tar+gzip', desc: 'Volume snapshot of the listed paths.' },
  { v: 'pgdump', label: 'pg_dump', desc: 'Postgres logical dump, optionally verified by a restore in a sidecar.' },
];

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
      <div className="seg" role="radiogroup" aria-label="Scope">
        {[
          { v: 'all',   label: 'All agents' },
          { v: 'node',  label: 'Pick nodes' },
          { v: 'label', label: 'By label' },
        ].map((opt) => (
          <button key={opt.v} type="button" role="radio" aria-checked={type === opt.v}
            onClick={() => onChange(opt.v, opt.v === 'all' ? '' : value)}
            className={type === opt.v ? 'seg-item-active' : 'seg-item'}
          >{opt.label}</button>
        ))}
      </div>

      {type === 'all' && (
        <p className="text-2xs text-zinc-500">
          Runs on every currently-connected agent ({nodes.length} {nodes.length === 1 ? 'node' : 'nodes'} right now).
        </p>
      )}

      {type === 'node' && (
        <div className="space-y-1.5 rounded-md bg-zinc-50 p-2 dark:bg-zinc-900/40">
          {nodes.length === 0 ? (
            <p className="text-2xs text-zinc-500">No agents are connected right now. The plan runs on whatever is online when it fires.</p>
          ) : nodes.map((n) => (
            <label key={n.id} className="flex cursor-pointer items-center gap-2 rounded px-1 py-0.5 text-xs hover:bg-zinc-100 dark:hover:bg-zinc-800">
              <input
                type="checkbox"
                checked={selected.has(n.hostname)}
                onChange={() => toggle(n.hostname)}
              />
              <span className="font-mono">{n.hostname}</span>
              {n.labels && Object.keys(n.labels).length > 0 && (
                <span className="text-3xs text-zinc-500">
                  {Object.entries(n.labels).map(([k, v]) => `${k}=${v}`).join(' · ')}
                </span>
              )}
            </label>
          ))}
          <p className="mt-2 text-3xs text-zinc-500">{selected.size} selected</p>
        </div>
      )}

      {type === 'label' && (
        <div>
          <input
            className="input font-mono text-xs"
            placeholder="role=worker"
            value={value}
            onChange={(e) => onChange('label', e.target.value)}
          />
          <p className="mt-1 text-2xs text-zinc-500">
            Format: <code>key=value</code>; matches any agent whose labels include the pair.
            {labelHints.size > 0 && (
              <> Examples from your fleet: {Array.from(labelHints).slice(0, 5).map((s, i) => (
                <button key={i} type="button" onClick={() => onChange('label', s)}
                  className="ml-1 rounded bg-zinc-100 px-1 font-mono text-3xs text-zinc-800 transition hover:bg-zinc-200 active:bg-zinc-300 dark:bg-zinc-800 dark:text-zinc-200 dark:hover:bg-zinc-700">{s}</button>
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
