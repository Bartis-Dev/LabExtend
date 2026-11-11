'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { api, errText } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import type { NodeView, MetricsSample } from '@/lib/types';
import { fmtBytes, fmtBps, fmtPct, fmtRelative, fmtUptime } from '@/lib/format';
import { AuthShell, useMe } from '@/components/auth-shell';
import { Modal } from '@/components/modal';
import { LabelChips } from '@/components/label-chips';
import { Server, Trash2 } from 'lucide-react';
import { EmptyState, ErrorState, TableSkeleton } from '@/components/feedback';
import { Tooltip } from '@/components/tooltip';

export default function NodesPage() {
  return (
    <AuthShell>
      <Nodes />
    </AuthShell>
  );
}

function Nodes() {
  const me = useMe();
  const [nodes, setNodes] = useState<NodeView[]>([]);
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);

  const load = async () => {
    try {
      const r = await api<{ nodes: NodeView[] }>('/api/nodes');
      setNodes(r.nodes ?? []);
      setErr(null);
    } catch (e: unknown) {
      setErr(errText(e, 'Could not load nodes. Check that the leader is reachable.'));
    } finally {
      setLoaded(true);
    }
  };

  useEffect(() => {
    load();
    const t = setInterval(load, 15_000);
    return () => clearInterval(t);
  }, []);

  useSSE({
    'node.metrics': (data: unknown) => {
      const m = data as MetricsSample;
      setNodes((prev) =>
        prev.map((n) => (n.id === m.node_id ? { ...n, metrics: m, status: 'online', last_seen: m.reported_at } : n))
      );
    },
    'node.connected': () => load(),
    'node.disconnected': (data: unknown) => {
      const d = data as { id: string };
      setNodes((prev) =>
        prev.map((n) => (n.id === d.id ? { ...n, status: 'offline', metrics: undefined } : n))
      );
    },
  });

  const sorted = useMemo(
    () => [...nodes].sort((a, b) => {
      // Online first, then by hostname — an offline node should not sit between
      // two live ones and make the list look unstable.
      if ((a.status === 'online') !== (b.status === 'online')) return a.status === 'online' ? -1 : 1;
      return a.hostname.localeCompare(b.hostname);
    }),
    [nodes],
  );
  const offline = nodes.filter((n) => n.status !== 'online').length;

  return (
    <div className="page">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Nodes</h1>
          <p className="page-sub">{nodes.length - offline} online · {offline} offline · live updates</p>
        </div>
        {offline > 0 && me.is_admin && (
          <button onClick={() => setCleanupOpen(true)} className="btn-ghost-danger">
            <Trash2 className="h-4 w-4" /> Clean up offline nodes
          </button>
        )}
      </header>

      {err && <ErrorState message={err} onRetry={load} />}

      {!loaded ? <TableSkeleton cols={6} /> : (
      <div className="card overflow-hidden p-0">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[48rem] text-sm">
            <thead className="border-b border-zinc-200 dark:border-zinc-800">
              <tr>
                <th className="th">Node</th>
                <th className="th">Labels</th>
                <th className="th w-[15%]">CPU</th>
                <th className="th w-[17%]">RAM</th>
                <th className="th w-[17%]">Disk</th>
                <th className="th w-[13%]">Network</th>
                <th className="th text-right">Uptime</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800/80">
              {sorted.map((n) => <NodeRow key={n.id} node={n} />)}
              {nodes.length === 0 && !err && (
                <tr>
                  <td colSpan={7}>
                    <EmptyState icon={<Server className="h-6 w-6" />} title="No nodes yet">
                      Start an agent with <code className="font-mono text-xs">LEADER_ADDR</code> pointing at this leader. It shows up here within seconds.
                    </EmptyState>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
      )}

      {cleanupOpen && (
        <CleanupModal
          offlineCount={offline}
          onClose={() => setCleanupOpen(false)}
          onDone={() => { setCleanupOpen(false); load(); }}
        />
      )}
    </div>
  );
}

function NodeRow({ node }: { node: NodeView }) {
  const router = useRouter();
  const m = node.metrics;
  const offline = node.status !== 'online';
  const href = `/node?id=${encodeURIComponent(node.id)}`;
  const diskFree = m ? Math.max(0, m.disk_total_bytes - m.disk_used_bytes) : 0;

  return (
    <tr className="row-link" onClick={() => router.push(href)}>
      <td className="td">
        <div className="flex items-center gap-3">
          <StatusDot offline={offline} />
          <div className="min-w-0">
            {/* The link carries the row for keyboard users; the row onClick is
                only a mouse convenience. */}
            <Link
              href={href}
              onClick={(e) => e.stopPropagation()}
              className="truncate font-medium hover:text-accent focus:outline-none focus-visible:underline"
            >
              {node.hostname}
            </Link>
            <div className="truncate text-2xs text-zinc-500">
              {node.os}/{node.arch}
              {m?.cpu_cores ? ` · ${m.cpu_cores} cores` : ''}
              {node.swarm_role ? ` · ${node.swarm_role}` : ''}
            </div>
          </div>
        </div>
      </td>

      <td className="td max-w-[22rem]">
        <LabelChips swarm={node.swarm_labels} agent={node.labels} />
      </td>

      {offline || !m ? (
        <td className="td text-xs text-zinc-500" colSpan={4}>Agent disconnected. No live metrics.</td>
      ) : (
        <>
          <td className="td"><MiniBar value={m.cpu_percent} detail={fmtPct(m.cpu_percent, 1)} /></td>
          <td className="td">
            <MiniBar value={m.mem_percent} detail={`${fmtBytes(m.mem_used_bytes)} / ${fmtBytes(m.mem_total_bytes)}`} />
          </td>
          <td className="td">
            <MiniBar value={m.disk_percent} detail={`${fmtBytes(diskFree)} free`} />
          </td>
          <td className="td whitespace-nowrap font-mono text-2xs text-zinc-500">
            <div>↓ {fmtBps(m.net_rx_bps)}</div>
            <div>↑ {fmtBps(m.net_tx_bps)}</div>
          </td>
        </>
      )}

      <td className="td text-right text-2xs text-zinc-500">
        {offline ? <>last seen<br />{fmtRelative(node.last_seen)}</> : (m?.uptime_seconds ? fmtUptime(m.uptime_seconds) : '-')}
      </td>
    </tr>
  );
}

function StatusDot({ offline }: { offline: boolean }) {
  if (offline) {
    return (
      <Tooltip label="Offline">
        <span className="h-2.5 w-2.5 shrink-0 rounded-full bg-zinc-400 dark:bg-zinc-600"><span className="sr-only">Offline</span></span>
      </Tooltip>
    );
  }
  return (
    <Tooltip label="Online">
      <span className="relative inline-flex h-2.5 w-2.5 shrink-0">
        <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75 motion-safe:animate-ping" />
        <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-emerald-500" />
        <span className="sr-only">Online</span>
      </span>
    </Tooltip>
  );
}

/** MiniBar is the row-sized variant of Bar: value on top, thin track below. */
function MiniBar({ value, detail }: { value: number; detail: string }) {
  const v = Math.max(0, Math.min(100, value || 0));
  const color = v >= 90 ? 'bg-red-500' : v >= 75 ? 'bg-amber-500' : 'bg-emerald-500';
  return (
    <div className="min-w-0">
      <div className="mb-1.5 truncate font-mono text-xs text-zinc-700 dark:text-zinc-300">{detail}</div>
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-200 dark:bg-zinc-800">
        <div className={`h-full rounded-full transition-all duration-300 ${color}`} style={{ width: `${v}%` }} />
      </div>
    </div>
  );
}

// ─── Cleanup modal ──────────────────────────────────────────────────────────

type Preset = 'all' | 'h1' | 'h6' | 'h24' | 'd7' | 'd30' | 'custom';

const PRESETS: { v: Exclude<Preset, 'custom'>; label: string; hours: number }[] = [
  { v: 'all',    label: 'All offline nodes (now)',     hours: 0 },
  { v: 'h1',     label: 'Offline for at least 1 hour',   hours: 1 },
  { v: 'h6',     label: 'Offline for at least 6 hours',  hours: 6 },
  { v: 'h24',    label: 'Offline for at least 24 hours', hours: 24 },
  { v: 'd7',     label: 'Offline for at least 7 days',   hours: 24 * 7 },
  { v: 'd30',    label: 'Offline for at least 30 days',  hours: 24 * 30 },
];

function CleanupModal({ offlineCount, onClose, onDone }: {
  offlineCount: number;
  onClose: () => void;
  onDone: () => void;
}) {
  const [preset, setPreset] = useState<Preset>('h24');
  // Kept as the raw text: a number input reports '' and 0 alike, and both
  // used to fall through to "delete every offline node".
  const [customHours, setCustomHours] = useState('48');
  const [busy, setBusy] = useState(false);
  const [confirmText, setConfirmText] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [result, setResult] = useState<{ deleted: number; ids: string[] } | null>(null);

  const dangerous = preset === 'all';
  const customValid = /^\d+$/.test(customHours.trim()) && Number(customHours) >= 1;
  const hours = preset === 'custom' ? Number(customHours) : (PRESETS.find((p) => p.v === preset)?.hours ?? 0);
  const inputOK = dangerous ? confirmText === 'DELETE' : preset !== 'custom' || customValid;

  const submit = async () => {
    if (!inputOK) return;
    setBusy(true);
    setErr(null);
    try {
      const r = await api<{ deleted: number; ids: string[] }>('/api/nodes/cleanup', {
        method: 'POST',
        body: dangerous ? { all: true } : { older_than_hours: hours },
      });
      setResult(r);
    } catch (e: unknown) {
      setErr(errText(e, 'Cleanup failed.'));
    } finally {
      setBusy(false);
    }
  };

  if (result) {
    return (
      <Modal title="Cleanup done" onClose={onDone} footer={<button onClick={onDone} className="btn-primary">Close</button>}>
        <p className="text-sm">{result.deleted === 1 ? '1 node deleted.' : `${result.deleted} nodes deleted.`}</p>
        {(result.ids ?? []).length > 0 && (
          <ul className="mt-3 max-h-48 space-y-1 overflow-y-auto rounded-md bg-zinc-50 p-2 font-mono text-2xs text-zinc-600 dark:bg-zinc-900/50 dark:text-zinc-400">
            {result.ids.map((id) => <li key={id}>{id}</li>)}
          </ul>
        )}
      </Modal>
    );
  }

  return (
    <Modal
      title="Delete offline nodes"
      subtitle={`${offlineCount} ${offlineCount === 1 ? 'node is' : 'nodes are'} offline right now. Online nodes are never deleted.`}
      onClose={onClose}
      dismissable={!busy}
      footer={
        <>
          <button onClick={onClose} disabled={busy} className="btn-ghost">Cancel</button>
          <button onClick={submit} disabled={busy || !inputOK} className={dangerous ? 'btn-danger' : 'btn-primary'}>
            {busy ? 'Deleting…' : 'Delete nodes'}
          </button>
        </>
      }
    >
      <div className="space-y-1 text-sm">
        {PRESETS.map((p) => (
          <label key={p.v} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800/60">
            <input type="radio" name="preset" checked={preset === p.v} onChange={() => setPreset(p.v)} />
            <span className={p.v === 'all' ? 'font-medium text-red-600 dark:text-red-400' : ''}>{p.label}</span>
          </label>
        ))}
        <label className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800/60">
          <input type="radio" name="preset" checked={preset === 'custom'} onChange={() => setPreset('custom')} />
          <span>Offline for at least</span>
          <input
            type="number"
            min={1}
            step={1}
            inputMode="numeric"
            className="input h-8 w-20 text-xs" aria-label="Hours offline"
            aria-invalid={preset === 'custom' && !customValid}
            aria-describedby="cleanup-hours-error"
            value={customHours}
            onChange={(e) => { setCustomHours(e.target.value); setPreset('custom'); }}
          />
          <span>hours</span>
        </label>
        {preset === 'custom' && !customValid && (
          <p id="cleanup-hours-error" role="alert" className="px-2 text-xs text-red-600 dark:text-red-400">
            Enter a whole number of hours, 1 or more. To delete every offline node, pick &quot;All offline nodes&quot;.
          </p>
        )}
      </div>

      {dangerous && (
        <div className="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-900 dark:border-red-900 dark:bg-red-950/40 dark:text-red-200">
          <strong>Warning:</strong> &quot;All offline nodes&quot; deletes EVERY node that is not connected right now.
          Type <code className="font-mono">DELETE</code> to confirm.
          <input
            className="input mt-2 h-8 font-mono text-xs" aria-label="Type DELETE to confirm"
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder="DELETE"
          />
        </div>
      )}

      {err && <p role="alert" className="alert-error mt-4">{err}</p>}
    </Modal>
  );
}
