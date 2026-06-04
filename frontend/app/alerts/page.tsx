'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import type { AlertHistoryRow, AlertRule, WebhookConfig } from '@/lib/types';
import { fmtAbsTime, fmtRelative } from '@/lib/format';
import { AuthShell } from '@/components/auth-shell';

const KINDS = [
  { v: 'cpu_percent',       label: 'CPU %' },
  { v: 'mem_percent',       label: 'RAM %' },
  { v: 'disk_percent',      label: 'Disk %' },
  { v: 'disk_free_gb',      label: 'Disk free (GB)' },
  { v: 'load_avg_1m',       label: 'Load avg 1m' },
  { v: 'node_offline',      label: 'Node offline (sec since last heartbeat)' },
  { v: 'container_crashed', label: 'Container in restart-loop' },
];

export default function AlertsPage() {
  return (
    <AuthShell>
      <Alerts />
    </AuthShell>
  );
}

function Alerts() {
  const [rules, setRules] = useState<AlertRule[]>([]);
  const [history, setHistory] = useState<AlertHistoryRow[]>([]);
  const [webhooks, setWebhooks] = useState<WebhookConfig[]>([]);
  const [editing, setEditing] = useState<Partial<AlertRule> | null>(null);

  const load = async () => {
    try {
      const r = await api<{ rules: AlertRule[] }>('/api/alert-rules');
      setRules(r.rules ?? []);
    } catch {/* */}
    try {
      const h = await api<{ history: AlertHistoryRow[] }>('/api/alert-history');
      setHistory(h.history ?? []);
    } catch {/* */}
    try {
      const w = await api<{ webhooks: WebhookConfig[] }>('/api/webhooks');
      setWebhooks(w.webhooks ?? []);
    } catch {/* */}
  };

  useEffect(() => {
    load();
    const t = setInterval(load, 30_000);
    return () => clearInterval(t);
  }, []);

  useSSE({
    'alert.triggered': () => load(),
    'alert.recovered': () => load(),
  });

  const save = async (r: Partial<AlertRule>) => {
    const payload = {
      name: r.name ?? '',
      kind: r.kind ?? 'cpu_percent',
      comparator: r.comparator ?? '>',
      threshold: Number(r.threshold ?? 0),
      duration_sec: Number(r.duration_sec ?? 60),
      scope: r.scope ?? 'all',
      webhook_id: r.webhook_id ?? '',
      cooldown_sec: Number(r.cooldown_sec ?? 300),
      enabled: r.enabled ?? true,
    };
    if (r.id) {
      await api(`/api/alert-rules/${r.id}`, { method: 'PUT', body: payload });
    } else {
      await api('/api/alert-rules', { method: 'POST', body: payload });
    }
    setEditing(null);
    load();
  };

  const remove = async (id: string) => {
    if (!confirm('Delete this alert rule?')) return;
    await api(`/api/alert-rules/${id}`, { method: 'DELETE' });
    load();
  };

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">Alerts</h1>
        <button onClick={() => setEditing({ enabled: true, comparator: '>', scope: 'all', cooldown_sec: 300, duration_sec: 60 })} className="btn-primary">
          New rule
        </button>
      </header>

      <div className="card overflow-hidden p-0">
        <table className="w-full text-sm">
          <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
            <tr>
              <th className="px-4 py-2">Name</th>
              <th className="px-4 py-2">Condition</th>
              <th className="px-4 py-2">Scope</th>
              <th className="px-4 py-2">Webhook</th>
              <th className="px-4 py-2">Status</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {rules.map((r) => {
              const wh = webhooks.find((w) => w.id === (r.webhook_id ?? ''));
              return (
                <tr key={r.id} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                  <td className="px-4 py-2 font-medium">{r.name}</td>
                  <td className="px-4 py-2 font-mono text-[12px]">{r.kind} {r.comparator} {r.threshold} for {r.duration_sec}s</td>
                  <td className="px-4 py-2 text-zinc-500">{r.scope}</td>
                  <td className="px-4 py-2 text-zinc-500">{wh ? wh.name : '—'}</td>
                  <td className="px-4 py-2"><span className={r.enabled ? 'badge-green' : 'badge-zinc'}>{r.enabled ? 'enabled' : 'disabled'}</span></td>
                  <td className="px-4 py-2 text-right">
                    <button onClick={() => setEditing(r)} className="btn-ghost">edit</button>
                    <button onClick={() => remove(r.id)} className="btn-ghost text-red-600 hover:text-red-700">delete</button>
                  </td>
                </tr>
              );
            })}
            {rules.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-6 text-center text-sm text-zinc-500">No rules yet.</td></tr>
            )}
          </tbody>
        </table>
      </div>

      <h2 className="mt-8 mb-3 text-sm font-semibold uppercase tracking-wide text-zinc-500">Recent fires</h2>
      <div className="card overflow-hidden p-0">
        <table className="w-full text-sm">
          <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
            <tr>
              <th className="px-4 py-2">When</th>
              <th className="px-4 py-2">Rule</th>
              <th className="px-4 py-2">State</th>
              <th className="px-4 py-2">Target</th>
              <th className="px-4 py-2">Value</th>
              <th className="px-4 py-2">Message</th>
            </tr>
          </thead>
          <tbody>
            {history.map((h) => (
              <tr key={h.id} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                <td className="px-4 py-2 text-zinc-500" title={fmtAbsTime(h.fired_at)}>{fmtRelative(h.fired_at)}</td>
                <td className="px-4 py-2">{h.rule_name || h.rule_id}</td>
                <td className="px-4 py-2"><span className={h.state === 'triggered' ? 'badge-red' : 'badge-green'}>{h.state}</span></td>
                <td className="px-4 py-2 font-mono text-[12px]">{[h.node_id, h.container_id].filter(Boolean).join(' / ') || '—'}</td>
                <td className="px-4 py-2 font-mono">{h.value?.toFixed?.(2) ?? '—'}</td>
                <td className="px-4 py-2 text-[12px] text-zinc-600 dark:text-zinc-400">{h.message}</td>
              </tr>
            ))}
            {history.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-6 text-center text-sm text-zinc-500">No alerts fired yet.</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {editing && (
        <RuleModal
          initial={editing}
          webhooks={webhooks}
          onCancel={() => setEditing(null)}
          onSave={save}
        />
      )}
    </div>
  );
}

function RuleModal({ initial, webhooks, onCancel, onSave }: {
  initial: Partial<AlertRule>;
  webhooks: WebhookConfig[];
  onCancel: () => void;
  onSave: (r: Partial<AlertRule>) => void;
}) {
  const [f, setF] = useState<Partial<AlertRule>>(initial);
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={onCancel}>
      <div className="card w-full max-w-lg" onClick={(e) => e.stopPropagation()}>
        <h3 className="mb-3 text-base font-semibold">{f.id ? 'Edit rule' : 'New rule'}</h3>
        <div className="space-y-3">
          <Field label="Name">
            <input className="input" value={f.name ?? ''} onChange={(e) => setF({ ...f, name: e.target.value })} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Metric">
              <select className="input" value={f.kind ?? 'cpu_percent'} onChange={(e) => setF({ ...f, kind: e.target.value })}>
                {KINDS.map((k) => <option key={k.v} value={k.v}>{k.label}</option>)}
              </select>
            </Field>
            <Field label="Compare">
              <select className="input" value={f.comparator ?? '>'} onChange={(e) => setF({ ...f, comparator: e.target.value as AlertRule['comparator'] })}>
                <option value=">">{'>'}</option>
                <option value=">=">{'>='}</option>
                <option value="<">{'<'}</option>
                <option value="<=">{'<='}</option>
              </select>
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Threshold">
              <input className="input" type="number" value={f.threshold ?? 0} onChange={(e) => setF({ ...f, threshold: Number(e.target.value) })} />
            </Field>
            <Field label="Duration (sec)">
              <input className="input" type="number" value={f.duration_sec ?? 60} onChange={(e) => setF({ ...f, duration_sec: Number(e.target.value) })} />
            </Field>
          </div>
          <Field label="Scope">
            <input className="input" value={f.scope ?? 'all'} onChange={(e) => setF({ ...f, scope: e.target.value })} placeholder="all | node:bd-manager | label:env=prod" />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Webhook">
              <select className="input" value={f.webhook_id ?? ''} onChange={(e) => setF({ ...f, webhook_id: e.target.value })}>
                <option value="">— none —</option>
                {webhooks.map((w) => <option key={w.id} value={w.id}>{w.name}</option>)}
              </select>
            </Field>
            <Field label="Cooldown (sec)">
              <input className="input" type="number" value={f.cooldown_sec ?? 300} onChange={(e) => setF({ ...f, cooldown_sec: Number(e.target.value) })} />
            </Field>
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={f.enabled ?? true} onChange={(e) => setF({ ...f, enabled: e.target.checked })} />
            Enabled
          </label>
        </div>
        <div className="mt-4 flex justify-end gap-2">
          <button onClick={onCancel} className="btn-ghost">Cancel</button>
          <button onClick={() => onSave(f)} className="btn-primary">Save</button>
        </div>
      </div>
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
