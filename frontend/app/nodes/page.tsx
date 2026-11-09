'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { api } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import type { NodeView, MetricsSample } from '@/lib/types';
import { fmtBytes, fmtBps, fmtPct, fmtRelative, fmtUptime } from '@/lib/format';
import { AuthShell } from '@/components/auth-shell';
import { Modal } from '@/components/modal';
import { LabelChips } from '@/components/label-chips';
import { Trash2 } from 'lucide-react';

export default function NodesPage() {
  return (
    <AuthShell>
      <Nodes />
    </AuthShell>
  );
}

function Nodes() {
  const [nodes, setNodes] = useState<NodeView[]>([]);
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const load = async () => {
    try {
      const r = await api<{ nodes: NodeView[] }>('/api/nodes');
      setNodes(r.nodes ?? []);
      setErr(null);
    } catch (e: unknown) {
      setErr((e as { body?: { error?: string } })?.body?.error ?? 'Could not load nodes.');
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
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Nodes</h1>
          <p className="text-xs text-zinc-500">{nodes.length - offline} online · {offline} offline · live updates</p>
        </div>
        {offline > 0 && (
          <button onClick={() => setCleanupOpen(true)} className="btn-ghost text-red-600 hover:bg-red-50 dark:hover:bg-red-950/40">
            <Trash2 className="h-3.5 w-3.5" /> cleanup offline nodes
          </button>
        )}
      </header>

      {err && (
        <div className="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
          {err}
        </div>
      )}

      <div className="card overflow-hidden p-0">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[860px] text-sm">
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
                  <td colSpan={7} className="px-3 py-12 text-center text-sm text-zinc-500">
                    No nodes yet. Start an agent with <code className="font-mono text-xs">LEADER_ADDR</code> pointing here.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

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
        <div className="flex items-center gap-2">
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
            <div className="truncate text-[11px] text-zinc-500">
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
        <td className="td text-xs text-zinc-500" colSpan={4}>Agent disconnected — no live metrics.</td>
      ) : (
        <>
          <td className="td"><MiniBar value={m.cpu_percent} detail={fmtPct(m.cpu_percent, 1)} /></td>
          <td className="td">
            <MiniBar value={m.mem_percent} detail={`${fmtBytes(m.mem_used_bytes)} / ${fmtBytes(m.mem_total_bytes)}`} />
          </td>
          <td className="td">
            <MiniBar value={m.disk_percent} detail={`${fmtBytes(diskFree)} free`} />
          </td>
          <td className="td font-mono text-[11px] text-zinc-500">
            <div>↓ {fmtBps(m.net_rx_bps)}</div>
            <div>↑ {fmtBps(m.net_tx_bps)}</div>
          </td>
        </>
      )}

      <td className="td text-right text-[11px] text-zinc-500">
        {offline ? <>last seen<br />{fmtRelative(node.last_seen)}</> : (m?.uptime_seconds ? fmtUptime(m.uptime_seconds) : '—')}
      </td>
    </tr>
  );
}

function StatusDot({ offline }: { offline: boolean }) {
  if (offline) {
    return <span className="h-2 w-2 shrink-0 rounded-full bg-zinc-400 dark:bg-zinc-600" title="offline" />;
  }
  return (
    <span className="relative inline-flex h-2 w-2 shrink-0" title="online">
      <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75 motion-safe:animate-ping" />
      <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" />
    </span>
  );
}

/** MiniBar is the row-sized variant of Bar: value on top, 4px track below. */
function MiniBar({ value, detail }: { value: number; detail: string }) {
  const v = Math.max(0, Math.min(100, value || 0));
  const color = v >= 90 ? 'bg-red-500' : v >= 75 ? 'bg-amber-500' : 'bg-emerald-500';
  return (
    <div className="min-w-0">
      <div className="mb-1 truncate font-mono text-[11px] text-zinc-700 dark:text-zinc-300">{detail}</div>
      <div className="h-1 w-full overflow-hidden rounded-full bg-zinc-200 dark:bg-zinc-800">
        <div className={`h-full rounded-full transition-all duration-300 ${color}`} style={{ width: `${v}%` }} />
      </div>
    </div>
  );
}

// ─── Cleanup modal ──────────────────────────────────────────────────────────

type Preset = 'all' | 'h1' | 'h6' | 'h24' | 'd7' | 'd30' | 'custom';

const PRESETS: { v: Preset; label: string; hours: number }[] = [
  { v: 'all',    label: 'alle offline (jetzt)',      hours: 0 },
  { v: 'h1',     label: 'offline seit ≥ 1 Stunde',   hours: 1 },
  { v: 'h6',     label: 'offline seit ≥ 6 Stunden',  hours: 6 },
  { v: 'h24',    label: 'offline seit ≥ 24 Stunden', hours: 24 },
  { v: 'd7',     label: 'offline seit ≥ 7 Tagen',    hours: 24 * 7 },
  { v: 'd30',    label: 'offline seit ≥ 30 Tagen',   hours: 24 * 30 },
];

function CleanupModal({ offlineCount, onClose, onDone }: {
  offlineCount: number;
  onClose: () => void;
  onDone: () => void;
}) {
  const [preset, setPreset] = useState<Preset>('h24');
  const [customHours, setCustomHours] = useState<number>(48);
  const [busy, setBusy] = useState(false);
  const [confirmText, setConfirmText] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [result, setResult] = useState<{ deleted: number; ids: string[] } | null>(null);

  const hours = preset === 'custom' ? customHours : (PRESETS.find((p) => p.v === preset)?.hours ?? 0);
  const dangerous = preset === 'all';
  const confirmOK = !dangerous || confirmText === 'DELETE';

  const submit = async () => {
    if (!confirmOK) return;
    setBusy(true);
    setErr(null);
    try {
      const r = await api<{ deleted: number; ids: string[] }>('/api/nodes/cleanup', {
        method: 'POST',
        body: { older_than_hours: hours },
      });
      setResult(r);
    } catch (e: unknown) {
      setErr((e as { body?: { error?: string } })?.body?.error ?? String(e));
    } finally {
      setBusy(false);
    }
  };

  if (result) {
    return (
      <Modal title="Cleanup done" onClose={onDone} footer={<button onClick={onDone} className="btn-primary">Close</button>}>
        <p className="text-sm">{result.deleted} Node(s) gelöscht.</p>
        {result.ids.length > 0 && (
          <ul className="mt-3 max-h-48 space-y-1 overflow-y-auto rounded-md bg-zinc-50 p-2 font-mono text-[11px] text-zinc-600 scroll-thin dark:bg-zinc-900/50 dark:text-zinc-400">
            {result.ids.map((id) => <li key={id}>{id}</li>)}
          </ul>
        )}
      </Modal>
    );
  }

  return (
    <Modal
      title="Offline-Nodes löschen"
      subtitle={`${offlineCount} Node(s) sind aktuell offline. Online-Nodes werden NIE gelöscht.`}
      onClose={onClose}
      footer={
        <>
          <button onClick={onClose} className="btn-ghost">Cancel</button>
          <button onClick={submit} disabled={busy || !confirmOK} className={dangerous ? 'btn-danger' : 'btn-primary'}>
            {busy ? 'Lösche…' : 'Cleanup'}
          </button>
        </>
      }
    >
      <div className="space-y-1 text-sm">
        {PRESETS.map((p) => (
          <label key={p.v} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800/60">
            <input type="radio" name="preset" className="accent-current" checked={preset === p.v} onChange={() => setPreset(p.v)} />
            <span className={p.v === 'all' ? 'font-medium text-red-600 dark:text-red-400' : ''}>{p.label}</span>
          </label>
        ))}
        <label className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800/60">
          <input type="radio" name="preset" className="accent-current" checked={preset === 'custom'} onChange={() => setPreset('custom')} />
          <span>offline seit ≥</span>
          <input
            type="number"
            min={1}
            className="input h-7 w-20 text-xs"
            value={customHours}
            onChange={(e) => { setCustomHours(+e.target.value); setPreset('custom'); }}
          />
          <span>Stunden</span>
        </label>
      </div>

      {dangerous && (
        <div className="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-900 dark:border-red-900 dark:bg-red-950/40 dark:text-red-200">
          <strong>Achtung:</strong> „alle offline" löscht JEDEN Node der gerade nicht verbunden ist.
          Tippe <code className="font-mono">DELETE</code> zum Bestätigen.
          <input
            className="input mt-2 h-7 font-mono text-xs"
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder="DELETE"
          />
        </div>
      )}

      {err && <p className="mt-3 text-sm text-red-600 dark:text-red-400">{err}</p>}
    </Modal>
  );
}
