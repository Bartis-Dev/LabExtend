'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { Box, RotateCcw } from 'lucide-react';
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

      {filtered.length === 0 ? (
        <div className="card py-12 text-center text-sm text-zinc-500">No containers match.</div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {filtered.map((c) => (
            <ContainerCard key={c.node_id + c.container_id} c={c} />
          ))}
        </div>
      )}
    </div>
  );
}

// parseImage splits a Docker image reference into its display parts and drops
// the sha256 digest (operators care about the tag, not the hash). Handles
// registry ports correctly — the tag colon is only the one after the last '/'.
//   postgres:16                       → { name: postgres, tag: 16 }
//   ghcr.io/acme/api:v2               → { namespace: ghcr.io/acme, name: api, tag: v2 }
//   registry:5000/x/app:v2@sha256:..  → { namespace: registry:5000/x, name: app, tag: v2, pinned }
//   redis@sha256:...                  → { name: redis, pinned: true }  (no tag)
function parseImage(image: string): { namespace: string; name: string; tag: string | null; pinned: boolean } {
  let ref = image || '';
  let pinned = false;
  const at = ref.indexOf('@');
  if (at >= 0) { ref = ref.slice(0, at); pinned = true; }

  const lastSlash = ref.lastIndexOf('/');
  const tagColon = ref.indexOf(':', lastSlash + 1);
  let repo = ref;
  let tag: string | null = null;
  if (tagColon >= 0) { repo = ref.slice(0, tagColon); tag = ref.slice(tagColon + 1); }

  const slash = repo.lastIndexOf('/');
  const name = slash >= 0 ? repo.slice(slash + 1) : repo;
  const namespace = slash >= 0 ? repo.slice(0, slash) : '';
  return { namespace, name, tag, pinned };
}

function ContainerCard({ c }: { c: ContainerView }) {
  const img = parseImage(c.image);
  const badgeCls =
    c.crashed_loop ? 'badge-red' :
    c.state === 'running' && c.health === 'unhealthy' ? 'badge-amber' :
    c.state === 'running' ? 'badge-green' :
    c.state === 'restarting' ? 'badge-amber' :
    'badge-zinc';
  const badgeLabel = c.crashed_loop ? `crashed ×${c.recent_restarts}` : (c.health || c.state || 'unknown');

  return (
    <Link
      href={`/container?node=${encodeURIComponent(c.node_id)}&id=${encodeURIComponent(c.container_id)}`}
      className="card group flex flex-col gap-3 transition hover:border-accent/50 hover:shadow-md"
    >
      {/* header: name + node + state */}
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate font-medium leading-tight transition group-hover:text-accent">{c.name}</div>
          <div className="mt-0.5 truncate text-[11px] text-zinc-500">{c.node_id}</div>
        </div>
        <span className={`${badgeCls} shrink-0`}>{badgeLabel}</span>
      </div>

      {/* image — the visual anchor of the card */}
      <div className="flex items-center gap-2.5 rounded-lg border border-zinc-100 bg-zinc-50 px-3 py-2 dark:border-zinc-800 dark:bg-zinc-950/40">
        <Box className="h-4 w-4 shrink-0 text-zinc-400" />
        <div className="min-w-0 flex-1">
          {img.namespace && (
            <div className="truncate font-mono text-[10px] leading-tight text-zinc-400">{img.namespace}/</div>
          )}
          <div className="truncate font-mono text-[13px] font-semibold text-zinc-800 dark:text-zinc-100">{img.name || '—'}</div>
        </div>
        {img.tag ? (
          <span className="badge shrink-0 bg-accent/10 font-mono text-accent">{img.tag}</span>
        ) : img.pinned ? (
          <span className="badge-zinc shrink-0 font-mono" title="pinned by digest">digest</span>
        ) : (
          <span className="badge-zinc shrink-0 font-mono text-zinc-400">latest</span>
        )}
      </div>

      {/* live metrics */}
      <div className="grid grid-cols-3 gap-2 border-t border-zinc-100 pt-2.5 dark:border-zinc-800">
        <Metric label="CPU" value={fmtPct(c.cpu_percent, 1)} />
        <Metric
          label="RAM"
          value={fmtBytes(c.mem_used_bytes)}
          sub={c.mem_limit_bytes > 0 ? `/ ${fmtBytes(c.mem_limit_bytes)}` : undefined}
        />
        <Metric label="Net" value={`↓ ${fmtBps(c.net_rx_bps)}`} sub={`↑ ${fmtBps(c.net_tx_bps)}`} />
      </div>

      {/* footer: uptime + restarts */}
      <div className="mt-auto flex items-center justify-between text-[11px] text-zinc-500">
        <span>{c.state === 'running' ? 'up' : c.state} · {fmtRelative(c.started_at_ms)}</span>
        {c.restart_count > 0 && (
          <span className="inline-flex items-center gap-1" title={`${c.restart_count} restarts total`}>
            <RotateCcw className="h-3 w-3" />{c.restart_count}
          </span>
        )}
      </div>
    </Link>
  );
}

function Metric({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[10px] uppercase tracking-wide text-zinc-400">{label}</div>
      <div className="truncate font-mono text-xs font-medium">{value}</div>
      {sub && <div className="truncate font-mono text-[10px] text-zinc-400">{sub}</div>}
    </div>
  );
}
