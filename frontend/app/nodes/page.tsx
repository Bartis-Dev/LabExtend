'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { api } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import type { NodeView, MetricsSample } from '@/lib/types';
import { fmtBytes, fmtBps, fmtPct, fmtRelative, fmtUptime } from '@/lib/format';
import { AuthShell } from '@/components/auth-shell';
import { Bar } from '@/components/bar';
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

  const load = async () => {
    try {
      const r = await api<{ nodes: NodeView[] }>('/api/nodes');
      setNodes(r.nodes ?? []);
    } catch {/* ignore */}
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

  const offline = nodes.filter((n) => n.status !== 'online').length;

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Nodes</h1>
          <p className="text-xs text-zinc-500">{nodes.length - offline} online · {offline} offline · live updates</p>
        </div>
        {offline > 0 && (
          <button onClick={() => setCleanupOpen(true)} className="btn-ghost text-red-600">
            <Trash2 className="h-3.5 w-3.5" /> cleanup offline nodes
          </button>
        )}
      </header>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {nodes.map((n) => <NodeRow key={n.id} node={n} />)}
        {nodes.length === 0 && (
          <div className="card text-sm text-zinc-500">No nodes yet.</div>
        )}
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
  const m = node.metrics;
  const offline = node.status !== 'online';
  const diskFree = m ? Math.max(0, m.disk_total_bytes - m.disk_used_bytes) : 0;

  return (
    <Link
      href={`/node?id=${encodeURIComponent(node.id)}`}
      className="card block transition hover:border-zinc-300 hover:shadow-sm dark:hover:border-zinc-700"
    >
      <div className="mb-3 flex items-start justify-between">
        <div>
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold">{node.hostname}</span>
            <span className={offline ? 'badge-zinc' : 'badge-green'}>
              {offline ? 'offline' : 'online'}
            </span>
          </div>
          <div className="text-[11px] text-zinc-500">
            {node.os}/{node.arch}
            {m?.cpu_cores ? ` · ${m.cpu_cores} cores` : ''}
            {m?.uptime_seconds ? ` · up ${fmtUptime(m.uptime_seconds)}` : ''}
          </div>
        </div>
        <div className="text-right text-[11px] text-zinc-500">
          {offline ? (
            <>last seen<br />{fmtRelative(node.last_seen)}</>
          ) : (
            <span className="inline-flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400">
              <span className="relative inline-flex h-2 w-2">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" />
              </span>
              live
            </span>
          )}
        </div>
      </div>

      {m && !offline ? (
        <div className="space-y-2.5">
          <Bar
            label="CPU"
            value={m.cpu_percent}
            detail={fmtPct(m.cpu_percent, 1)}
          />
          <Bar
            label="RAM"
            value={m.mem_percent}
            detail={`${fmtBytes(m.mem_used_bytes)} / ${fmtBytes(m.mem_total_bytes)}`}
          />
          <Bar
            label="Disk"
            value={m.disk_percent}
            detail={`${fmtBytes(diskFree)} free · ${fmtBytes(m.disk_total_bytes)}`}
          />
          <div className="grid grid-cols-2 gap-2 pt-1 text-[11px] text-zinc-500">
            <div>↓ {fmtBps(m.net_rx_bps)}</div>
            <div className="text-right">↑ {fmtBps(m.net_tx_bps)}</div>
          </div>
        </div>
      ) : (
        <div className="rounded-md bg-zinc-100 px-3 py-4 text-xs text-zinc-500 dark:bg-zinc-800/60">
          No live metrics. Agent disconnected.
        </div>
      )}
    </Link>
  );
}

// ─── Cleanup modal ──────────────────────────────────────────────────────────

type Preset = 'all' | 'h1' | 'h6' | 'h24' | 'd7' | 'd30' | 'custom';

const PRESETS: { v: Preset; label: string; hours: number }[] = [
  { v: 'all',    label: 'alle offline (jetzt)',     hours: 0 },
  { v: 'h1',     label: 'offline seit ≥ 1 Stunde',  hours: 1 },
  { v: 'h6',     label: 'offline seit ≥ 6 Stunden', hours: 6 },
  { v: 'h24',    label: 'offline seit ≥ 24 Stunden', hours: 24 },
  { v: 'd7',     label: 'offline seit ≥ 7 Tagen',   hours: 24 * 7 },
  { v: 'd30',    label: 'offline seit ≥ 30 Tagen',  hours: 24 * 30 },
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

  const hours = preset === 'custom' ? customHours : (PRESETS.find((p) => p.v === preset)?.hours ?? 0);
  const dangerous = preset === 'all';
  const confirmOK = !dangerous || confirmText === 'DELETE';

  const submit = async () => {
    if (!confirmOK) return;
    setBusy(true);
    try {
      const r = await api<{ deleted: number; ids: string[] }>('/api/nodes/cleanup', {
        method: 'POST',
        body: { older_than_hours: hours },
      });
      alert(`${r.deleted} Node(s) gelöscht.${r.deleted > 0 ? '\n\n' + r.ids.join('\n') : ''}`);
      onDone();
    } catch (e: unknown) {
      alert('failed: ' + ((e as { body?: { error?: string } })?.body?.error ?? String(e)));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={onClose}>
      <div className="card w-full max-w-md" onClick={(e) => e.stopPropagation()}>
        <h3 className="mb-1 text-base font-semibold">Offline-Nodes löschen</h3>
        <p className="mb-4 text-xs text-zinc-500">
          {offlineCount} Node(s) sind aktuell offline. Online-Nodes werden NIE gelöscht.
        </p>

        <div className="space-y-1.5 text-sm">
          {PRESETS.map((p) => (
            <label key={p.v} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 hover:bg-zinc-100 dark:hover:bg-zinc-900">
              <input type="radio" name="preset" checked={preset === p.v} onChange={() => setPreset(p.v)} />
              <span className={p.v === 'all' ? 'font-medium text-red-600' : ''}>{p.label}</span>
            </label>
          ))}
          <label className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 hover:bg-zinc-100 dark:hover:bg-zinc-900">
            <input type="radio" name="preset" checked={preset === 'custom'} onChange={() => setPreset('custom')} />
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
          <div className="mt-4 rounded-md bg-red-50 p-3 text-xs text-red-900 dark:bg-red-900/30 dark:text-red-200">
            <strong>Achtung:</strong> "alle offline" löscht JEDEN Node der gerade nicht verbunden ist.
            Tippe <code>DELETE</code> zum Bestätigen.
            <input
              className="input mt-2 h-7 font-mono text-xs"
              value={confirmText}
              onChange={(e) => setConfirmText(e.target.value)}
              placeholder="DELETE"
            />
          </div>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button onClick={onClose} className="btn-ghost">Cancel</button>
          <button
            onClick={submit}
            disabled={busy || !confirmOK}
            className={dangerous ? 'btn-danger' : 'btn-primary'}
          >
            {busy ? 'Lösche…' : 'Cleanup'}
          </button>
        </div>
      </div>
    </div>
  );
}
