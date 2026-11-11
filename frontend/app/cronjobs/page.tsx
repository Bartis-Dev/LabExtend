'use client';

import { useEffect, useRef, useState } from 'react';
import { Clock } from 'lucide-react';
import { api, errText } from '@/lib/api';
import { AuthShell } from '@/components/auth-shell';
import type { NodeView } from '@/lib/types';
import { SchedulePicker } from '@/components/schedule-picker';
import { toast } from '@/components/toast';
import { Modal } from '@/components/modal';
import { Field, inputCls } from '@/components/field';
import { confirmDialog } from '@/components/confirm';
import { EmptyState, ErrorState, TableSkeleton } from '@/components/feedback';
import { Tooltip } from '@/components/tooltip';

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

export default function CronJobsPage() {
  return <AuthShell><Cron /></AuthShell>;
}

function Cron() {
  const [nodes, setNodes] = useState<NodeView[]>([]);
  const [jobs, setJobs] = useState<CronJob[]>([]);
  const [editing, setEditing] = useState<Partial<CronJob> | null>(null);
  const [filterNode, setFilterNode] = useState('');
  const [loaded, setLoaded] = useState(false);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [formErr, setFormErr] = useState<string | null>(null);
  const [fieldErr, setFieldErr] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);

  // Only the newest load may write: switching the node filter while a slow
  // list runs must not show the previous node's jobs.
  const seq = useRef(0);
  const load = async () => {
    const my = ++seq.current;
    try {
      const n = await api<{ nodes: NodeView[] }>('/api/nodes');
      if (my !== seq.current) return;
      setNodes(n.nodes ?? []);
      const url = filterNode ? `/api/nodes/${encodeURIComponent(filterNode)}/cronjobs` : '/api/cronjobs';
      const j = await api<{ jobs: CronJob[] }>(url);
      if (my !== seq.current) return;
      setJobs(j.jobs ?? []);
      setLoadErr(null);
    } catch (e) {
      if (my !== seq.current) return;
      setLoadErr(errText(e, 'Could not load cron jobs.'));
    } finally {
      if (my === seq.current) setLoaded(true);
    }
  };
  useEffect(() => { load(); }, [filterNode]);

  const openEditor = (j: Partial<CronJob>) => {
    setFormErr(null);
    setFieldErr({});
    setEditing(j);
  };

  // The job is stored even when its node could not take it, so "saved" alone
  // would claim something the host is not doing.
  const reportApply = (r: ApplyResult, node: string, done: string) => {
    if (r.applied === false) {
      toast.error(`${done}, but not applied to ${node}: ${r.apply_error ?? 'unknown error'}`);
    } else {
      toast.success(`${done} and applied to ${node}.`);
    }
  };

  const save = async (j: Partial<CronJob>) => {
    const errs: Record<string, string> = {};
    if (!j.node_id) errs.node_id = 'Pick the node this job runs on.';
    if (!j.command?.trim()) errs.command = 'Enter the command to run.';
    if (!j.schedule?.trim() || j.schedule.trim().split(/\s+/).length !== 5) errs.schedule = 'Use five cron fields: min hour day-of-month month day-of-week.';
    setFieldErr(errs);
    if (Object.keys(errs).length) return;

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
      setFormErr(errText(e));
    } finally {
      setSaving(false);
    }
  };
  const remove = async (j: CronJob) => {
    const ok = await confirmDialog({
      title: 'Delete this cron job?',
      body: <>The line <code className="font-mono text-xs">{j.schedule} {j.command}</code> is removed from <code className="font-mono text-xs">/etc/cron.d/bpm</code> on {j.node_id}.</>,
      confirmLabel: 'Delete job',
      danger: true,
    });
    if (!ok) return;
    try {
      const r = await api<ApplyResult>(`/api/cronjobs/${j.id}`, { method: 'DELETE' });
      reportApply(r, j.node_id, 'Deleted');
      load();
    } catch (e) {
      toast.error(errText(e));
    }
  };
  const apply = async (node_id: string) => {
    try {
      await api(`/api/nodes/${encodeURIComponent(node_id)}/cronjobs/apply`, { method: 'POST' });
      toast.success(`Applied to ${node_id}.`);
    } catch (e) {
      toast.error(`Not applied to ${node_id}: ${errText(e)}`);
    }
  };

  const newJob = () => openEditor({ enabled: true, run_as: 'root', schedule: '0 3 * * *' });

  return (
    <div className="page">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Cron jobs</h1>
          <p className="page-sub">Written atomically to <code>/etc/cron.d/bpm</code> on each agent.</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <select className="input w-48" aria-label="Filter by node" value={filterNode} onChange={(e) => setFilterNode(e.target.value)}>
            <option value="">All nodes</option>
            {nodes.map((n) => <option key={n.id} value={n.id}>{n.hostname}</option>)}
          </select>
          <button onClick={newJob} className="btn-primary">New job</button>
        </div>
      </header>

      {loadErr && <ErrorState message={loadErr} onRetry={load} />}

      {!loaded ? <TableSkeleton cols={5} /> : (
      <div className="card overflow-x-auto p-0">
        <table className="w-full min-w-[44rem] text-sm">
          <thead className="border-b border-zinc-200 dark:border-zinc-800">
            <tr>
              <th className="th">Node</th>
              <th className="th">Schedule</th>
              <th className="th">Command</th>
              <th className="th">User</th>
              <th className="th">Status</th>
              <th className="th"><span className="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800">
            {jobs.map((j) => (
              <tr key={j.id}>
                <td className="td font-mono text-xs">{j.node_id}</td>
                <td className="td font-mono text-xs">{j.schedule}</td>
                <td className="td font-mono text-xs">{j.command}</td>
                <td className="td text-zinc-500">{j.run_as}</td>
                <td className="td"><span className={j.enabled ? 'badge-green' : 'badge-zinc'}>{j.enabled ? 'enabled' : 'disabled'}</span></td>
                <td className="td whitespace-nowrap text-right">
                  <Tooltip label="Write the cron file to the host again">
                    <button onClick={() => apply(j.node_id)} className="btn-ghost btn-sm">Apply</button>
                  </Tooltip>
                  <button onClick={() => openEditor(j)} className="btn-ghost btn-sm">Edit</button>
                  <button onClick={() => remove(j)} className="btn-ghost-danger btn-sm">Delete</button>
                </td>
              </tr>
            ))}
            {jobs.length === 0 && !loadErr && (
              <tr><td colSpan={6}>
                <EmptyState
                  icon={<Clock className="h-6 w-6" />}
                  title={filterNode ? 'No cron jobs on this node' : 'No cron jobs yet'}
                  action={<button onClick={newJob} className="btn-primary">New job</button>}
                >
                  Jobs run on the host as the chosen user, independent of containers.
                </EmptyState>
              </td></tr>
            )}
          </tbody>
        </table>
      </div>
      )}

      {editing && (
        <Modal
          title={editing.id ? 'Edit job' : 'New job'}
          onClose={() => setEditing(null)}
          dismissable={!saving}
          footer={
            <>
              <button type="button" onClick={() => setEditing(null)} disabled={saving} className="btn-ghost">Cancel</button>
              <button type="submit" form="cron-form" disabled={saving} className="btn-primary">
                {saving ? 'Saving…' : 'Save'}
              </button>
            </>
          }
        >
          <form id="cron-form" noValidate onSubmit={(e) => { e.preventDefault(); save(editing); }} className="space-y-4 text-sm">
            <Field label="Node" error={fieldErr.node_id}>
              <select className={inputCls(fieldErr.node_id)} value={editing.node_id ?? ''} onChange={(e) => setEditing({ ...editing, node_id: e.target.value })}>
                <option value="">Select a node</option>
                {nodes.map((n) => <option key={n.id} value={n.id}>{n.hostname}</option>)}
              </select>
            </Field>
            <Field label="Schedule" group error={fieldErr.schedule}>
              <SchedulePicker
                variant="cron"
                value={editing.schedule ?? '0 3 * * *'}
                onChange={(v) => setEditing((cur) => (cur ? { ...cur, schedule: v } : cur))}
              />
            </Field>
            <Field label="Command" error={fieldErr.command}>
              <input className={inputCls(fieldErr.command, 'font-mono text-xs')} placeholder="echo hello >> /tmp/log" value={editing.command ?? ''} onChange={(e) => setEditing({ ...editing, command: e.target.value })} />
            </Field>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field label="User">
                <input className="input" value={editing.run_as ?? 'root'} onChange={(e) => setEditing({ ...editing, run_as: e.target.value })} />
              </Field>
              <Field label="Comment">
                <input className="input" value={editing.comment ?? ''} onChange={(e) => setEditing({ ...editing, comment: e.target.value })} />
              </Field>
            </div>
            <label className="flex cursor-pointer items-center gap-2">
              <input type="checkbox" checked={editing.enabled ?? true} onChange={(e) => setEditing({ ...editing, enabled: e.target.checked })} />
              Enabled
            </label>
            {formErr && <p role="alert" className="alert-error">{formErr}</p>}
          </form>
        </Modal>
      )}
    </div>
  );
}
