'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { api } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import type { NodeView, MetricsSample } from '@/lib/types';
import { fmtBytes, fmtBps, fmtPct, fmtRelative, fmtUptime } from '@/lib/format';
import { AuthShell } from '@/components/auth-shell';

export default function NodesPage() {
  return (
    <AuthShell>
      <Nodes />
    </AuthShell>
  );
}

function Nodes() {
  const [nodes, setNodes] = useState<NodeView[]>([]);

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

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-4">
        <h1 className="text-2xl font-semibold tracking-tight">Nodes</h1>
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
        Total disk shown is the root filesystem mounted at <code>/</code>. Bytes/sec are leader-computed deltas between heartbeats.
      </p>
    </div>
  );
}
