'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { api } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import type { ContainerView } from '@/lib/types';
import { fmtBytes, fmtBps, fmtPct, fmtRelative } from '@/lib/format';
import { AuthShell } from '@/components/auth-shell';

export default function ContainersPage() {
  return (
    <AuthShell>
      <Containers />
    </AuthShell>
  );
}

type Filter = 'all' | 'running' | 'stopped' | 'crashed';

function Containers() {
  const [items, setItems] = useState<ContainerView[]>([]);
  const [filter, setFilter] = useState<Filter>('all');
  const [search, setSearch] = useState('');

  const load = async () => {
    try {
      const r = await api<{ containers: ContainerView[] }>('/api/containers');
      setItems(r.containers ?? []);
    } catch {/* */}
  };

  useEffect(() => {
    load();
    const t = setInterval(load, 15_000);
    return () => clearInterval(t);
  }, []);

  useSSE({
    'containers.update': (data: unknown) => {
      const u = data as { node_id: string; containers: ContainerView[] };
      setItems((prev) => {
        const map = new Map(prev.map((c) => [c.node_id + '::' + c.container_id, c]));
        for (const c of u.containers ?? []) map.set(c.node_id + '::' + c.container_id, c);
        return Array.from(map.values());
      });
    },
  });

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return items.filter((c) => {
      if (filter === 'running' && c.state !== 'running') return false;
      if (filter === 'stopped' && c.state === 'running') return false;
      if (filter === 'crashed' && !c.crashed_loop) return false;
      if (q && !(c.name.toLowerCase().includes(q) || c.image.toLowerCase().includes(q) || c.node_id.toLowerCase().includes(q))) return false;
      return true;
    });
  }, [items, filter, search]);

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Containers</h1>
          <p className="text-xs text-zinc-500">{items.length} across {new Set(items.map((c) => c.node_id)).size} nodes</p>
        </div>
        <div className="flex items-center gap-2">
          <input
            placeholder="filter…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="input w-56"
          />
          <div className="flex overflow-hidden rounded-md border border-zinc-300 text-xs dark:border-zinc-700">
            {(['all','running','stopped','crashed'] as Filter[]).map((f) => (
              <button
                key={f}
                onClick={() => setFilter(f)}
                className={
                  f === filter
                    ? 'bg-zinc-900 px-2 py-1 text-white dark:bg-zinc-100 dark:text-zinc-900'
                    : 'px-2 py-1 text-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-800'
                }
              >{f}</button>
            ))}
          </div>
        </div>
      </header>

      <div className="card overflow-hidden p-0">
        <table className="w-full text-sm">
          <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
            <tr>
              <th className="px-4 py-2">Name</th>
              <th className="px-4 py-2">Node</th>
              <th className="px-4 py-2">State</th>
              <th className="px-4 py-2">CPU</th>
              <th className="px-4 py-2">RAM</th>
              <th className="px-4 py-2">Net ↓/↑</th>
              <th className="px-4 py-2 text-right">Started</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((c) => (
              <tr key={c.node_id + c.container_id} className="border-b border-zinc-100 last:border-0 hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-900">
                <td className="px-4 py-2">
                  <Link href={`/container?node=${encodeURIComponent(c.node_id)}&id=${encodeURIComponent(c.container_id)}`} className="font-medium hover:underline">
                    {c.name}
                  </Link>
                  <div className="text-[11px] text-zinc-500">{c.image}</div>
                </td>
                <td className="px-4 py-2 text-zinc-500">{c.node_id}</td>
                <td className="px-4 py-2">
                  <span className={
                    c.crashed_loop ? 'badge-red' :
                    c.state === 'running' && c.health === 'unhealthy' ? 'badge-amber' :
                    c.state === 'running' ? 'badge-green' :
                    c.state === 'restarting' ? 'badge-amber' :
                    'badge-zinc'
                  }>
                    {c.crashed_loop ? `crashed ×${c.recent_restarts}` : (c.health || c.state)}
                  </span>
                </td>
                <td className="px-4 py-2 font-mono">{fmtPct(c.cpu_percent, 1)}</td>
                <td className="px-4 py-2 font-mono text-[12px]">{fmtBytes(c.mem_used_bytes)}{c.mem_limit_bytes > 0 ? ` / ${fmtBytes(c.mem_limit_bytes)}` : ''}</td>
                <td className="px-4 py-2 font-mono text-[12px]">{fmtBps(c.net_rx_bps)} / {fmtBps(c.net_tx_bps)}</td>
                <td className="px-4 py-2 text-right text-[11px] text-zinc-500">{fmtRelative(c.started_at_ms)}</td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr><td colSpan={7} className="px-4 py-6 text-center text-sm text-zinc-500">No containers match.</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
