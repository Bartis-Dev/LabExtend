'use client';

import { useEffect, useRef, useState } from 'react';
import { ScrollText } from 'lucide-react';
import { api, errText } from '@/lib/api';
import { EmptyState, ErrorState, TableSkeleton } from '@/components/feedback';
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
  const [loaded, setLoaded] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const seq = useRef(0);

  // Only the newest request may write: a slow answer for "nod" must not
  // overwrite the one for "node_path".
  const load = async () => {
    const my = ++seq.current;
    try {
      const q = new URLSearchParams({ limit: '200' });
      if (action) q.set('action', action);
      const r = await api<{ audit: AuditRow[] }>('/api/audit?' + q.toString());
      if (my !== seq.current) return;
      setRows(r.audit ?? []);
      setErr(null);
    } catch (e) {
      if (my !== seq.current) return;
      setErr(errText(e, 'Could not load the audit log.'));
    } finally {
      if (my === seq.current) setLoaded(true);
    }
  };
  useEffect(() => {
    const t = setTimeout(load, action ? 300 : 0);
    return () => clearTimeout(t);
  }, [action]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="page">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Audit log</h1>
          <p className="page-sub">Newest 200 entries.</p>
        </div>
        <input className="input w-full sm:w-80" aria-label="Filter by action" placeholder="filter action (e.g. node_path.create)" value={action} onChange={(e) => setAction(e.target.value)} />
      </header>
      {err && <ErrorState message={err} onRetry={load} />}
      {!loaded ? <TableSkeleton cols={6} /> : (
      <div className="card overflow-x-auto p-0">
        <table className="w-full min-w-[56rem] text-sm">
          <thead className="border-b border-zinc-200 dark:border-zinc-800">
            <tr>
              <th className="th">When</th>
              <th className="th">Actor</th>
              <th className="th">IP</th>
              <th className="th">Action</th>
              <th className="th">Target</th>
              <th className="th">Details</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                <td className="td text-zinc-500" title={fmtAbsTime(r.ts)}>{fmtRelative(r.ts)}</td>
                <td className="td">{r.actor_email || '-'}</td>
                <td className="td font-mono text-2xs text-zinc-500">{r.source_ip || '-'}</td>
                <td className="td font-mono text-xs">{r.action}</td>
                <td className="td font-mono text-2xs text-zinc-500">{[r.target_kind, r.target_id].filter(Boolean).join(':')}</td>
                <td className="td font-mono text-2xs text-zinc-500">{r.details ? JSON.stringify(r.details) : '-'}</td>
              </tr>
            ))}
            {rows.length === 0 && !err && (
              <tr><td colSpan={6}>
                <EmptyState icon={<ScrollText className="h-6 w-6" />} title={action ? 'No entries match this action' : 'No audit entries yet'}>
                  {action ? 'Clear or change the filter to see more.' : 'Changes made through LabExtend are recorded here.'}
                </EmptyState>
              </td></tr>
            )}
          </tbody>
        </table>
      </div>
      )}
    </div>
  );
}
