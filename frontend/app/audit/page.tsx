'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { AuthShell } from '@/components/auth-shell';
import { fmtAbsTime, fmtRelative } from '@/lib/format';

interface AuditRow {
  id: number; ts: number;
  actor_id?: number; actor_email?: string;
  source_ip?: string; action: string;
  target_kind?: string; target_id?: string;
  details?: unknown;
}

export default function AuditPage() {
  return <AuthShell><Audit /></AuthShell>;
}

function Audit() {
  const [rows, setRows] = useState<AuditRow[]>([]);
  const [action, setAction] = useState('');

  const load = async () => {
    try {
      const q = new URLSearchParams({ limit: '200' });
      if (action) q.set('action', action);
      setRows((await api<{ audit: AuditRow[] }>('/api/audit?' + q.toString())).audit ?? []);
    } catch { /* */ }
  };
  useEffect(() => { load(); }, [action]);

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">Audit log</h1>
        <input className="input w-56" placeholder="filter action (e.g. file.chown)" value={action} onChange={(e) => setAction(e.target.value)} />
      </header>
      <div className="card overflow-hidden p-0">
        <table className="w-full text-sm">
          <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
            <tr>
              <th className="px-4 py-2">When</th>
              <th className="px-4 py-2">Actor</th>
              <th className="px-4 py-2">IP</th>
              <th className="px-4 py-2">Action</th>
              <th className="px-4 py-2">Target</th>
              <th className="px-4 py-2">Details</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                <td className="px-4 py-2 text-zinc-500" title={fmtAbsTime(r.ts)}>{fmtRelative(r.ts)}</td>
                <td className="px-4 py-2">{r.actor_email || '—'}</td>
                <td className="px-4 py-2 font-mono text-[11px] text-zinc-500">{r.source_ip || '—'}</td>
                <td className="px-4 py-2 font-mono text-[12px]">{r.action}</td>
                <td className="px-4 py-2 font-mono text-[11px] text-zinc-500">{[r.target_kind, r.target_id].filter(Boolean).join(':')}</td>
                <td className="px-4 py-2 font-mono text-[11px] text-zinc-500">{r.details ? JSON.stringify(r.details) : '—'}</td>
              </tr>
            ))}
            {rows.length === 0 && <tr><td colSpan={6} className="px-4 py-6 text-center text-zinc-500">No audit entries.</td></tr>}
          </tbody>
        </table>
      </div>
    </div>
  );
}
