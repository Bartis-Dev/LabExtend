'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { api } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import { fmtBytes, fmtBps, fmtPct, fmtRelative, fmtUptime } from '@/lib/format';
import type { NodeView, MetricsSample } from '@/lib/types';
import { Bar } from '@/components/bar';
import { AuthShell } from '@/components/auth-shell';

export default function DashboardPage() {
  return (
    <AuthShell>
      <Dashboard />
    </AuthShell>
  );
}

function Dashboard() {
  const [nodes, setNodes] = useState<NodeView[]>([]);

  const load = useCallback(async () => {
    try {
      const r = await api<{ nodes: NodeView[] }>('/api/nodes');
      setNodes(r.nodes ?? []);
    } catch {
      // ignore (handled at shell)
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 15_000);
    return () => clearInterval(t);
  }, [load]);

  useSSE({
    'node.metrics': (data: unknown) => {
      const m = data as MetricsSample;
      setNodes((prev) =>
        prev.map((n) => (n.id === m.node_id ? { ...n, metrics: m, status: 'online' } : n))
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

  const online = nodes.filter((n) => n.status === 'online').length;

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-6 flex items-end justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Dashboard</h1>
          <p className="text-sm text-zinc-500">
            {online}/{nodes.length} nodes online · live metrics
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs text-zinc-500">
          <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-emerald-500" /> live
        </div>
      </header>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {nodes.map((n) => (
          <NodeCard key={n.id} node={n} />
        ))}
        {nodes.length === 0 && (
          <div className="card text-sm text-zinc-500">
            No nodes connected yet. The agent connects on boot — once it does, it appears here within a few seconds.
          </div>
        )}
      </div>
    </div>
  );
}

function NodeCard({ node }: { node: NodeView }) {
  const m = node.metrics;
  const offline = node.status !== 'online';
  return (
    <Link href={`/node?id=${encodeURIComponent(node.id)}`} className="card transition hover:shadow-md">
      <div className="mb-3 flex items-start justify-between">
        <div>
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold">{node.hostname}</span>
            <span className={offline ? 'badge-zinc' : 'badge-green'}>
              {offline ? 'offline' : 'online'}
            </span>
          </div>
          <div className="text-[11px] text-zinc-500">
            {node.os}/{node.arch} · uptime {fmtUptime(m?.uptime_seconds)}
          </div>
        </div>
        <div className="text-right text-[11px] text-zinc-500">
          last seen<br />{fmtRelative(node.last_seen)}
        </div>
      </div>

      {m && !offline ? (
        <div className="space-y-2.5">
          <Bar label="CPU" value={m.cpu_percent} detail={fmtPct(m.cpu_percent, 1) + ` · ${m.cpu_cores} cores`} />
          <Bar
            label="RAM"
            value={m.mem_percent}
            detail={`${fmtBytes(m.mem_used_bytes)} / ${fmtBytes(m.mem_total_bytes)}`}
          />
          <Bar
            label="Disk"
            value={m.disk_percent}
            detail={`${fmtBytes(m.disk_used_bytes)} / ${fmtBytes(m.disk_total_bytes)}`}
          />
          <div className="grid grid-cols-2 gap-2 pt-1 text-[11px] text-zinc-500">
            <div>↓ {fmtBps(m.net_rx_bps)}</div>
            <div className="text-right">↑ {fmtBps(m.net_tx_bps)}</div>
          </div>
        </div>
      ) : (
        <div className="rounded-md bg-zinc-100 px-3 py-4 text-xs text-zinc-500 dark:bg-zinc-800/60">
          No live metrics. Agent may be disconnected.
        </div>
      )}
    </Link>
  );
}
