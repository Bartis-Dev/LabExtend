'use client';

import { Suspense, useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { api } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import { fmtBytes, fmtBps, fmtPct, fmtRelative, fmtUptime } from '@/lib/format';
import type { ContainerView, MetricsBucket, MetricsSample, NodeView } from '@/lib/types';
import { Bar } from '@/components/bar';
import { AuthShell } from '@/components/auth-shell';
import Link from 'next/link';

export default function NodePage() {
  return (
    <AuthShell>
      <Suspense fallback={<div className="p-8 text-sm text-zinc-500">Loading…</div>}>
        <NodeDetail />
      </Suspense>
    </AuthShell>
  );
}

type Window = 'live' | '1h' | '12h' | '24h';

function NodeDetail() {
  const params = useSearchParams();
  const id = params.get('id') ?? '';
  const [node, setNode] = useState<NodeView | null>(null);
  const [containers, setContainers] = useState<ContainerView[]>([]);
  const [window, setWindow] = useState<Window>('live');
  const [buckets, setBuckets] = useState<MetricsBucket[]>([]);

  const load = useCallback(async () => {
    if (!id) return;
    try {
      const n = await api<NodeView>(`/api/nodes/${encodeURIComponent(id)}`);
      setNode(n);
    } catch {/* */}
    try {
      const r = await api<{ containers: ContainerView[] }>('/api/containers');
      setContainers((r.containers ?? []).filter((c) => c.node_id === id));
    } catch {/* */}
  }, [id]);

  const loadBuckets = useCallback(async (w: Window) => {
    if (!id || w === 'live') {
      setBuckets([]);
      return;
    }
    const seconds = w === '1h' ? 3600 : w === '12h' ? 43200 : 86400;
    const since = Math.floor(Date.now() / 1000) - seconds;
    try {
      const r = await api<{ buckets: MetricsBucket[] }>(`/api/nodes/${encodeURIComponent(id)}/history?since=${since}`);
      setBuckets(r.buckets ?? []);
    } catch {/* */}
  }, [id]);

  useEffect(() => {
    load();
    const t = setInterval(load, 15_000);
    return () => clearInterval(t);
  }, [load]);

  useEffect(() => { loadBuckets(window); }, [window, loadBuckets]);

  useSSE({
    'node.metrics': (data: unknown) => {
      const m = data as MetricsSample;
      if (m.node_id === id) setNode((n) => (n ? { ...n, metrics: m, status: 'online' } : n));
    },
    'containers.update': (data: unknown) => {
      const u = data as { node_id: string; containers: ContainerView[] };
      if (u.node_id === id) {
        setContainers((prev) => {
          const map = new Map(prev.map((c) => [c.container_id, c]));
          for (const c of u.containers ?? []) map.set(c.container_id, c);
          return Array.from(map.values());
        });
      }
    },
  });

  const m = window === 'live' ? node?.metrics : avgBuckets(buckets, node?.metrics);

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      {!node ? (
        <div className="text-sm text-zinc-500">Loading…</div>
      ) : (
        <>
          <header className="mb-4 flex items-end justify-between">
            <div>
              <h1 className="text-2xl font-semibold tracking-tight">{node.hostname}</h1>
              <p className="text-xs text-zinc-500">{node.os}/{node.arch} · v{node.version || '—'}</p>
            </div>
            <span className={node.status === 'online' ? 'badge-green' : 'badge-zinc'}>{node.status}</span>
          </header>

          <div className="mb-2 flex items-center gap-2 text-xs">
            {(['live','1h','12h','24h'] as Window[]).map((w) => (
              <button
                key={w}
                onClick={() => setWindow(w)}
                className={
                  w === window
                    ? 'rounded-md bg-zinc-900 px-2 py-1 text-white dark:bg-zinc-100 dark:text-zinc-900'
                    : 'rounded-md px-2 py-1 text-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-800'
                }
              >
                {w === 'live' ? 'Live' : `Ø ${w}`}
              </button>
            ))}
            {window !== 'live' && (
              <span className="ml-2 text-[11px] text-zinc-500">{buckets.length} samples</span>
            )}
          </div>

          <div className="card">
            {m ? (
              <div className="space-y-3">
                <Bar label="CPU" value={m.cpu_percent} detail={fmtPct(m.cpu_percent, 1) + (node.metrics ? ` · ${node.metrics.cpu_cores} cores` : '')} />
                <Bar label="RAM" value={m.mem_percent} detail={node.metrics ? `${fmtBytes(node.metrics.mem_used_bytes)} / ${fmtBytes(node.metrics.mem_total_bytes)}` : fmtPct(m.mem_percent, 1)} />
                <Bar label="Disk" value={m.disk_percent} detail={node.metrics ? `${fmtBytes(node.metrics.disk_used_bytes)} / ${fmtBytes(node.metrics.disk_total_bytes)}` : fmtPct(m.disk_percent, 1)} />
                <div className="grid grid-cols-2 gap-4 pt-2 text-xs text-zinc-500">
                  <div>↓ Net <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBps(m.net_rx_bps)}</span></div>
                  <div className="text-right">↑ Net <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBps(m.net_tx_bps)}</span></div>
                  <div>Disk read <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBps(m.disk_read_bps)}</span></div>
                  <div className="text-right">Disk write <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBps(m.disk_write_bps)}</span></div>
                </div>
                {node.metrics && (
                  <div className="grid grid-cols-3 gap-2 pt-3 text-[11px] text-zinc-500">
                    <div>Uptime <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtUptime(node.metrics.uptime_seconds)}</span></div>
                    <div>Load 1m <span className="font-mono text-zinc-700 dark:text-zinc-300">{node.metrics.load_avg_1m.toFixed(2)}</span></div>
                    <div className="text-right">Last seen <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtRelative(node.last_seen)}</span></div>
                  </div>
                )}
              </div>
            ) : (
              <div className="text-sm text-zinc-500">No metrics for window.</div>
            )}
          </div>

          <h2 className="mt-8 mb-3 text-sm font-semibold uppercase tracking-wide text-zinc-500">Containers ({containers.length})</h2>
          <div className="card overflow-hidden p-0">
            <table className="w-full text-sm">
              <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
                <tr>
                  <th className="px-4 py-2">Name</th>
                  <th className="px-4 py-2">State</th>
                  <th className="px-4 py-2">CPU</th>
                  <th className="px-4 py-2">RAM</th>
                  <th className="px-4 py-2">Net ↓/↑</th>
                  <th className="px-4 py-2 text-right">Started</th>
                </tr>
              </thead>
              <tbody>
                {containers.map((c) => <ContainerRow key={c.container_id} c={c} />)}
                {containers.length === 0 && (
                  <tr><td colSpan={6} className="px-4 py-6 text-center text-sm text-zinc-500">No containers reported for this node.</td></tr>
                )}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}

function ContainerRow({ c }: { c: ContainerView }) {
  const stateBadge =
    c.crashed_loop ? 'badge-red' :
    c.state === 'running' && c.health === 'healthy' ? 'badge-green' :
    c.state === 'running' ? 'badge-green' :
    c.state === 'restarting' ? 'badge-amber' :
    'badge-zinc';
  const stateLabel = c.crashed_loop ? `crashed ×${c.recent_restarts}/60s` : (c.health || c.state);
  return (
    <tr className="border-b border-zinc-100 last:border-0 hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-900">
      <td className="px-4 py-2">
        <Link href={`/container?node=${encodeURIComponent(c.node_id)}&id=${encodeURIComponent(c.container_id)}`} className="font-medium hover:underline">{c.name}</Link>
        <div className="text-[11px] text-zinc-500">{c.image}</div>
      </td>
      <td className="px-4 py-2"><span className={stateBadge}>{stateLabel}</span></td>
      <td className="px-4 py-2 font-mono">{fmtPct(c.cpu_percent, 1)}</td>
      <td className="px-4 py-2 font-mono text-[12px]">{fmtBytes(c.mem_used_bytes)}{c.mem_limit_bytes > 0 ? ` / ${fmtBytes(c.mem_limit_bytes)}` : ''}</td>
      <td className="px-4 py-2 font-mono text-[12px]">{fmtBps(c.net_rx_bps)} / {fmtBps(c.net_tx_bps)}</td>
      <td className="px-4 py-2 text-right text-[11px] text-zinc-500">{fmtRelative(c.started_at_ms)}</td>
    </tr>
  );
}

// avgBuckets returns a synthetic MetricsSample representing the bucket average.
function avgBuckets(buckets: MetricsBucket[], fallback?: MetricsSample): MetricsSample | undefined {
  if (buckets.length === 0) return fallback;
  let cpu = 0, mem = 0, disk = 0, nrx = 0, ntx = 0, drd = 0, dwr = 0;
  for (const b of buckets) {
    cpu += b.cpu_percent;
    mem += b.mem_percent;
    disk += b.disk_percent;
    nrx += b.net_rx_bps;
    ntx += b.net_tx_bps;
    drd += b.disk_read_bps;
    dwr += b.disk_write_bps;
  }
  const n = buckets.length;
  return {
    ...(fallback ?? ({} as MetricsSample)),
    cpu_percent: cpu / n,
    mem_percent: mem / n,
    disk_percent: disk / n,
    net_rx_bps: Math.round(nrx / n),
    net_tx_bps: Math.round(ntx / n),
    disk_read_bps: Math.round(drd / n),
    disk_write_bps: Math.round(dwr / n),
  } as MetricsSample;
}
