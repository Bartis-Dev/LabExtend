'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { api } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import type { NodeView, MetricsSample } from '@/lib/types';
import { fmtBytes, fmtBps, fmtPct, fmtRelative, fmtUptime } from '@/lib/format';
import { AuthShell } from '@/components/auth-shell';
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
        prev.map((n) => (n.id === m.node_id ? { ...n, metrics: m, status: 'online' } : n))
      );
    },
  });

  const offline = nodes.filter((n) => n.status !== 'online').length;

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Nodes</h1>
          <p className="text-xs text-zinc-500">{nodes.length - offline} online · {offline} offline</p>
        </div>
        {offline > 0 && (
          <button onClick={() => setCleanupOpen(true)} className="btn-ghost text-red-600">
            <Trash2 className="h-3.5 w-3.5" /> cleanup offline nodes
          </button>
        )}
      </header>

      <div className="card overflow-hidden p-0">
        <table className="w-full text-sm">
          <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
            <tr>
              <th className="px-4 py-2">Hostname</th>
              <th className="px-4 py-2">Status</th>
              <th className="px-4 py-2">OS</th>
              <th className="px-4 py-2">CPU</th>
              <th className="px-4 py-2">RAM</th>
              <th className="px-4 py-2">Disk</th>
              <th className="px-4 py-2">Net ↓/↑</th>
              <th className="px-4 py-2">Uptime</th>
              <th className="px-4 py-2 text-right">Last seen</th>
            </tr>
          </thead>
          <tbody>
            {nodes.map((n) => {
              const m = n.metrics;
              return (
                <tr key={n.id} className="border-b border-zinc-100 last:border-0 hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-900">
                  <td className="px-4 py-2 font-medium">
                    <Link href={`/node?id=${encodeURIComponent(n.id)}`} className="hover:underline">
                      {n.hostname}
                    </Link>
                  </td>
                  <td className="px-4 py-2">
                    <span className={n.status === 'online' ? 'badge-green' : 'badge-zinc'}>{n.status}</span>
                  </td>
                  <td className="px-4 py-2 text-zinc-500">{n.os}/{n.arch}</td>
                  <td className="px-4 py-2 font-mono">{fmtPct(m?.cpu_percent ?? 0, 1)}</td>
                  <td className="px-4 py-2 font-mono">{fmtPct(m?.mem_percent ?? 0, 1)}</td>
                  <td className="px-4 py-2 font-mono">{fmtPct(m?.disk_percent ?? 0, 1)}</td>
                  <td className="px-4 py-2 font-mono text-[12px]">
                    {fmtBps(m?.net_rx_bps ?? 0)} / {fmtBps(m?.net_tx_bps ?? 0)}
                  </td>
                  <td className="px-4 py-2 text-zinc-500">{fmtUptime(m?.uptime_seconds)}</td>
                  <td className="px-4 py-2 text-right text-zinc-500">{fmtRelative(n.last_seen)}</td>
                </tr>
              );
            })}
            {nodes.length === 0 && (
              <tr><td colSpan={9} className="px-4 py-6 text-center text-sm text-zinc-500">No nodes yet.</td></tr>
            )}
          </tbody>
        </table>
      </div>
      <p className="mt-3 text-[11px] text-zinc-500">
        Total disk shown is the root filesystem mounted at <code>/</code>. Bytes/sec are leader-computed deltas between heartbeats.{' '}
        <span className="text-zinc-400">Cleanup nimmt nur Nodes raus die JETZT offline UND idle ≥ Schwelle sind — online Nodes bleiben.</span>
      </p>

      {cleanupOpen && (
        <CleanupModal
          offlineCount={offline}
          onClose={() => setCleanupOpen(false)}
          onDone={() => { setCleanupOpen(false); load(); }}
        />
      )}

      {/* fmtBytes kept warm for future detail columns */}
      <span className="hidden">{fmtBytes(0)}</span>
    </div>
  );
}

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
          {offlineCount} Node(s) sind aktuell offline. Online-Nodes werden NIE gelöscht — der Filter prüft Live-Status + last_seen.
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
            <strong>Achtung:</strong> "alle offline" löscht JEDEN Node der gerade nicht verbunden ist —
            auch welche die in 1 Min wieder online sind. Tippe <code>DELETE</code> zum Bestätigen.
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
