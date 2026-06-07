'use client';

import { Suspense, useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import Link from 'next/link';
import { api } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import { fmtBytes, fmtBps, fmtPct, fmtRelative, fmtUptime } from '@/lib/format';
import type { ContainerView, MetricsBucket, MetricsSample, NodeView } from '@/lib/types';
import { Bar } from '@/components/bar';
import { AuthShell } from '@/components/auth-shell';
import { MetricChart } from '@/components/metric-chart';

export default function NodePage() {
  return (
    <AuthShell>
      <Suspense fallback={<div className="p-8 text-sm text-zinc-500">Loading…</div>}>
        <NodeDetail />
      </Suspense>
    </AuthShell>
  );
}

type Window = '5m' | '1h' | '6h' | '24h';

const WINDOWS: { v: Window; label: string; spanSec: number; stepSec: number; source: 'samples' | 'buckets' }[] = [
  { v: '5m',  label: '5 min',  spanSec:    300, stepSec:    3, source: 'samples' },
  { v: '1h',  label: '1 h',    spanSec:   3600, stepSec:   15, source: 'samples' },
  { v: '6h',  label: '6 h',    spanSec:  21600, stepSec:  300, source: 'buckets' },
  { v: '24h', label: '24 h',   spanSec:  86400, stepSec:  900, source: 'buckets' },
];

function NodeDetail() {
  const params = useSearchParams();
  const id = params.get('id') ?? '';
  const [node, setNode] = useState<NodeView | null>(null);
  const [containers, setContainers] = useState<ContainerView[]>([]);
  const [window, setWindow] = useState<Window>('1h');
  const [buckets, setBuckets] = useState<MetricsBucket[]>([]);
  const [containerFilter, setContainerFilter] = useState('');

  const loadHeader = useCallback(async () => {
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

  const loadSeries = useCallback(async (w: Window) => {
    if (!id) return;
    const cfg = WINDOWS.find((x) => x.v === w)!;
    const now = Date.now();
    try {
      if (cfg.source === 'samples') {
        const sinceMs = now - cfg.spanSec * 1000;
        const r = await api<{ samples: MetricsBucket[] }>(
          `/api/nodes/${encodeURIComponent(id)}/samples?since_ms=${sinceMs}&until_ms=${now}`
        );
        setBuckets(downsample(r.samples ?? [], cfg.stepSec));
      } else {
        const sinceSec = Math.floor(now / 1000) - cfg.spanSec;
        const untilSec = Math.floor(now / 1000);
        const r = await api<{ buckets: MetricsBucket[] }>(
          `/api/nodes/${encodeURIComponent(id)}/history?since=${sinceSec}&until=${untilSec}`
        );
        setBuckets(downsample(r.buckets ?? [], cfg.stepSec));
      }
    } catch { setBuckets([]); }
  }, [id]);

  useEffect(() => {
    loadHeader();
    const t = setInterval(loadHeader, 15_000);
    return () => clearInterval(t);
  }, [loadHeader]);

  useEffect(() => {
    loadSeries(window);
    const everyMs = window === '5m' ? 3000 : window === '1h' ? 15000 : 60000;
    const t = setInterval(() => loadSeries(window), everyMs);
    return () => clearInterval(t);
  }, [window, loadSeries]);

  useSSE({
    'node.metrics': (data: unknown) => {
      const m = data as MetricsSample;
      if (m.node_id === id) {
        setNode((n) => (n ? { ...n, metrics: m, status: 'online', last_seen: m.reported_at } : n));
        if (window === '5m') {
          setBuckets((prev) => {
            const next = [...prev, sampleToBucket(m)];
            return next.length > 200 ? next.slice(-200) : next;
          });
        }
      }
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

  if (!node) return <div className="p-8 text-sm text-zinc-500">Loading…</div>;

  const m = node.metrics;
  const diskFree = m ? Math.max(0, m.disk_total_bytes - m.disk_used_bytes) : 0;
  const timestamps = buckets.map((b) => b.bucket_minute);

  const filteredContainers = containerFilter
    ? containers.filter((c) =>
        c.name.toLowerCase().includes(containerFilter.toLowerCase()) ||
        c.image.toLowerCase().includes(containerFilter.toLowerCase()))
    : containers;

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{node.hostname}</h1>
          <p className="text-xs text-zinc-500">
            {node.os}/{node.arch} · v{node.version || '—'}
            {m?.cpu_cores ? ` · ${m.cpu_cores} cores` : ''}
            {m?.uptime_seconds ? ` · up ${fmtUptime(m.uptime_seconds)}` : ''}
            {node.status === 'online'
              ? ''
              : ' · last seen ' + fmtRelative(node.last_seen)}
          </p>
        </div>
        <span className={node.status === 'online' ? 'badge-green' : 'badge-zinc'}>{node.status}</span>
      </header>

      <div className="card">
        {m ? (
          <div className="space-y-3">
            <Bar label="CPU" value={m.cpu_percent} detail={fmtPct(m.cpu_percent, 1)} />
            <Bar label="RAM" value={m.mem_percent} detail={`${fmtBytes(m.mem_used_bytes)} / ${fmtBytes(m.mem_total_bytes)}`} />
            <Bar label="Disk" value={m.disk_percent} detail={`${fmtBytes(diskFree)} free · ${fmtBytes(m.disk_total_bytes)}`} />
            <div className="grid grid-cols-2 gap-4 pt-2 text-xs text-zinc-500">
              <div>↓ Net <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBps(m.net_rx_bps)}</span></div>
              <div className="text-right">↑ Net <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBps(m.net_tx_bps)}</span></div>
              <div>Disk read <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBps(m.disk_read_bps)}</span></div>
              <div className="text-right">Disk write <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBps(m.disk_write_bps)}</span></div>
            </div>
          </div>
        ) : (
          <div className="text-sm text-zinc-500">No live metrics.</div>
        )}
      </div>

      <div className="mt-6 mb-2 flex items-center gap-2 text-xs">
        <span className="text-zinc-500">Window:</span>
        {WINDOWS.map((w) => (
          <button
            key={w.v}
            onClick={() => setWindow(w.v)}
            className={
              w.v === window
                ? 'rounded-md bg-zinc-900 px-2.5 py-1 text-white dark:bg-zinc-100 dark:text-zinc-900'
                : 'rounded-md px-2.5 py-1 text-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-800'
            }
          >{w.label}</button>
        ))}
        <span className="ml-2 text-[11px] text-zinc-500">
          {buckets.length} pts · step {WINDOWS.find((w) => w.v === window)!.stepSec}s
        </span>
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <div className="card">
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-zinc-500">CPU / RAM / Disk (%)</h3>
          <MetricChart
            timestamps={timestamps}
            yMax={100}
            formatY={(v) => v.toFixed(0) + '%'}
            series={[
              { label: 'CPU',  color: '#3B82F6', values: buckets.map((b) => b.cpu_percent) },
              { label: 'RAM',  color: '#10B981', values: buckets.map((b) => b.mem_percent) },
              { label: 'Disk', color: '#F59E0B', values: buckets.map((b) => b.disk_percent) },
            ]}
          />
        </div>
        <div className="card">
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-zinc-500">Network (B/s)</h3>
          <MetricChart
            timestamps={timestamps}
            formatY={(v) => fmtBps(v).replace('/s', '')}
            series={[
              { label: '↓ rx', color: '#06B6D4', values: buckets.map((b) => b.net_rx_bps) },
              { label: '↑ tx', color: '#8B5CF6', values: buckets.map((b) => b.net_tx_bps) },
            ]}
          />
        </div>
        <div className="card">
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-zinc-500">Disk I/O (B/s)</h3>
          <MetricChart
            timestamps={timestamps}
            formatY={(v) => fmtBps(v).replace('/s', '')}
            series={[
              { label: 'read',  color: '#22C55E', values: buckets.map((b) => b.disk_read_bps) },
              { label: 'write', color: '#EF4444', values: buckets.map((b) => b.disk_write_bps) },
            ]}
          />
        </div>
      </div>

      <div className="mt-8 mb-2 flex items-end justify-between">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-zinc-500">
          Containers ({filteredContainers.length})
        </h2>
        <input
          placeholder="filter…"
          value={containerFilter}
          onChange={(e) => setContainerFilter(e.target.value)}
          className="input w-56"
        />
      </div>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {filteredContainers.map((c) => <ContainerCard key={c.container_id} c={c} />)}
        {filteredContainers.length === 0 && (
          <div className="card text-sm text-zinc-500">No containers.</div>
        )}
      </div>
    </div>
  );
}

function ContainerCard({ c }: { c: ContainerView }) {
  const stateBadge =
    c.crashed_loop ? 'badge-red' :
    c.state === 'running' && c.health === 'unhealthy' ? 'badge-amber' :
    c.state === 'running' ? 'badge-green' :
    c.state === 'restarting' ? 'badge-amber' :
    'badge-zinc';
  const stateLabel = c.crashed_loop ? `crashed ×${c.recent_restarts}/60s` : (c.health || c.state);

  return (
    <Link
      href={`/container?node=${encodeURIComponent(c.node_id)}&id=${encodeURIComponent(c.container_id)}`}
      className="card block transition hover:border-zinc-300 hover:shadow-sm dark:hover:border-zinc-700"
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold">{c.name}</div>
          <div className="truncate text-[11px] text-zinc-500">{c.image}</div>
        </div>
        <span className={stateBadge + ' shrink-0'}>{stateLabel}</span>
      </div>
      {c.state === 'running' ? (
        <div className="space-y-1.5">
          <Bar label="CPU" value={c.cpu_percent} detail={fmtPct(c.cpu_percent, 1)} />
          <Bar
            label="RAM"
            value={c.mem_percent}
            detail={`${fmtBytes(c.mem_used_bytes)}${c.mem_limit_bytes > 0 ? ` / ${fmtBytes(c.mem_limit_bytes)}` : ''}`}
            tone={c.mem_limit_bytes > 0 ? 'auto' : 'cool'}
          />
          <div className="grid grid-cols-2 gap-2 pt-1 text-[11px] text-zinc-500">
            <div>↓ {fmtBps(c.net_rx_bps)}</div>
            <div className="text-right">↑ {fmtBps(c.net_tx_bps)}</div>
          </div>
        </div>
      ) : (
        <div className="rounded-md bg-zinc-100 px-3 py-2 text-[11px] text-zinc-500 dark:bg-zinc-800/60">
          {c.state === 'exited' ? `exit ${c.exit_code} · ${fmtRelative(c.finished_at_ms)}` : c.state}
        </div>
      )}
    </Link>
  );
}

// ─── helpers ────────────────────────────────────────────────────────────────

function downsample(buckets: MetricsBucket[], stepSec: number): MetricsBucket[] {
  if (buckets.length === 0 || stepSec <= 1) return buckets;
  const out: MetricsBucket[] = [];
  let bucketStart = Math.floor(buckets[0].bucket_minute / stepSec) * stepSec;
  let acc = newAccum(bucketStart);
  let n = 0;
  for (const s of buckets) {
    const slot = Math.floor(s.bucket_minute / stepSec) * stepSec;
    if (slot !== bucketStart) {
      if (n > 0) out.push(divide(acc, n));
      bucketStart = slot;
      acc = newAccum(bucketStart);
      n = 0;
    }
    acc.cpu_percent += s.cpu_percent;
    acc.mem_percent += s.mem_percent;
    acc.disk_percent += s.disk_percent;
    acc.net_rx_bps += s.net_rx_bps;
    acc.net_tx_bps += s.net_tx_bps;
    acc.disk_read_bps += s.disk_read_bps;
    acc.disk_write_bps += s.disk_write_bps;
    n++;
  }
  if (n > 0) out.push(divide(acc, n));
  return out;
}

function newAccum(ts: number): MetricsBucket {
  return {
    bucket_minute: ts, cpu_percent: 0, mem_percent: 0, disk_percent: 0,
    net_rx_bps: 0, net_tx_bps: 0, disk_read_bps: 0, disk_write_bps: 0,
  };
}
function divide(b: MetricsBucket, n: number): MetricsBucket {
  return {
    bucket_minute: b.bucket_minute,
    cpu_percent: b.cpu_percent / n,
    mem_percent: b.mem_percent / n,
    disk_percent: b.disk_percent / n,
    net_rx_bps: Math.round(b.net_rx_bps / n),
    net_tx_bps: Math.round(b.net_tx_bps / n),
    disk_read_bps: Math.round(b.disk_read_bps / n),
    disk_write_bps: Math.round(b.disk_write_bps / n),
  };
}

function sampleToBucket(m: MetricsSample): MetricsBucket {
  return {
    bucket_minute: m.reported_at,
    cpu_percent: m.cpu_percent,
    mem_percent: m.mem_percent,
    disk_percent: m.disk_percent,
    net_rx_bps: m.net_rx_bps,
    net_tx_bps: m.net_tx_bps,
    disk_read_bps: m.disk_read_bps,
    disk_write_bps: m.disk_write_bps,
  };
}
