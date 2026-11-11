'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Boxes, ChevronDown, ChevronRight, Layers, RotateCcw, Server } from 'lucide-react';
import { api, errText } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import type { ContainerView } from '@/lib/types';
import { fmtBytes, fmtBps, fmtPct, fmtRelative } from '@/lib/format';
import { AuthShell } from '@/components/auth-shell';
import { EmptyState, ErrorState, TableSkeleton } from '@/components/feedback';
import { Tooltip } from '@/components/tooltip';

export default function ContainersPage() {
  return (
    <AuthShell>
      <Containers />
    </AuthShell>
  );
}

type Filter = 'all' | 'running' | 'stopped' | 'crashed';
type GroupBy = 'node' | 'stack' | 'none';

const GROUP_STORAGE_KEY = 'labextend.containers.groupBy';

/**
 * stackOf derives the deployment a container belongs to.
 *
 * Swarm sets com.docker.stack.namespace, plain Compose sets
 * com.docker.compose.project. Anything started by hand has neither and lands in
 * a single "no stack" bucket rather than one bucket per container.
 */
function stackOf(c: ContainerView): string | null {
  const l = c.labels ?? {};
  return l['com.docker.stack.namespace'] || l['com.docker.compose.project'] || null;
}

function Containers() {
  const [items, setItems] = useState<ContainerView[]>([]);
  const [filter, setFilter] = useState<Filter>('all');
  const [search, setSearch] = useState('');
  const [groupBy, setGroupBy] = useState<GroupBy>('node');
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [err, setErr] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);

  // Remember the grouping choice per browser; it is a view preference, not
  // state anyone else needs to see.
  useEffect(() => {
    try {
      const saved = localStorage.getItem(GROUP_STORAGE_KEY);
      if (saved === 'node' || saved === 'stack' || saved === 'none') setGroupBy(saved);
    } catch { /* private mode / blocked storage */ }
  }, []);
  const chooseGroup = (g: GroupBy) => {
    setGroupBy(g);
    try { localStorage.setItem(GROUP_STORAGE_KEY, g); } catch { /* ignore */ }
  };

  const load = async () => {
    try {
      const r = await api<{ containers: ContainerView[] }>('/api/containers');
      setItems(r.containers ?? []);
      setErr(null);
    } catch (e: unknown) {
      setErr(errText(e, 'Could not load containers. Check that the leader is reachable.'));
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
      if (q) {
        const stack = stackOf(c) ?? '';
        const hay = `${c.name} ${c.image} ${c.node_id} ${stack}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
  }, [items, filter, search]);

  const groups = useMemo(() => {
    const sortRows = (rows: ContainerView[]) =>
      [...rows].sort((a, b) => {
        // Restart loops first because they need attention, then the live
        // containers, and anything stopped last — a deliberately stopped
        // container is not a problem and should not outrank a running one.
        const rank = (c: ContainerView) => (c.crashed_loop ? 0 : c.state === 'running' ? 1 : 2);
        return rank(a) - rank(b) || a.name.localeCompare(b.name);
      });

    if (groupBy === 'none') return [{ key: '', label: '', rows: sortRows(filtered) }];

    const buckets = new Map<string, ContainerView[]>();
    for (const c of filtered) {
      const key = groupBy === 'node' ? c.node_id : (stackOf(c) ?? '￿no stack');
      const arr = buckets.get(key);
      if (arr) arr.push(c);
      else buckets.set(key, [c]);
    }
    // The ￿ prefix keeps the "no stack" bucket last without a special case.
    return Array.from(buckets.entries())
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([key, rows]) => ({
        key,
        label: key.startsWith('￿') ? key.slice(1) : key,
        rows: sortRows(rows),
      }));
  }, [filtered, groupBy]);

  const nodeCount = new Set(items.map((c) => c.node_id)).size;
  const stackCount = new Set(items.map((c) => stackOf(c)).filter(Boolean)).size;

  return (
    <div className="page">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Containers</h1>
          <p className="page-sub">
            {items.length} across {nodeCount} node{nodeCount === 1 ? '' : 's'}
            {stackCount > 0 && ` · ${stackCount} stack${stackCount === 1 ? '' : 's'}`}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <input
            placeholder="filter name, image, node, stack…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="input w-full sm:w-72"
            aria-label="Filter containers"
          />
          <div className="seg" role="group" aria-label="State filter">
            {(['all', 'running', 'stopped', 'crashed'] as Filter[]).map((f) => (
              <button
                key={f}
                onClick={() => setFilter(f)}
                aria-pressed={f === filter}
                className={f === filter ? 'seg-item-active' : 'seg-item'}
              >{f}</button>
            ))}
          </div>
          <div className="seg" role="group" aria-label="Grouping">
            <button onClick={() => chooseGroup('node')} aria-pressed={groupBy === 'node'}
              className={groupBy === 'node' ? 'seg-item-active' : 'seg-item'}>by node</button>
            <button onClick={() => chooseGroup('stack')} aria-pressed={groupBy === 'stack'}
              className={groupBy === 'stack' ? 'seg-item-active' : 'seg-item'}>by stack</button>
            <button onClick={() => chooseGroup('none')} aria-pressed={groupBy === 'none'}
              className={groupBy === 'none' ? 'seg-item-active' : 'seg-item'}>all</button>
          </div>
        </div>
      </header>

      {err && <ErrorState message={err} onRetry={load} />}

      {!loaded ? (
        <TableSkeleton cols={7} />
      ) : filtered.length === 0 ? (
        <div className="card p-0">
          {items.length === 0 ? (
            <EmptyState icon={<Boxes className="h-6 w-6" />} title="No containers reported yet">
              Agents report their containers every few seconds once they are connected.
            </EmptyState>
          ) : (
            <EmptyState
              title="No containers match the current filter"
              action={<button className="btn-ghost" onClick={() => { setSearch(''); setFilter('all'); }}>Clear filters</button>}
            />
          )}
        </div>
      ) : (
        <div className="space-y-5">
          {groups.map((g) => (
            <GroupBlock
              key={g.key || 'flat'}
              groupBy={groupBy}
              label={g.label}
              rows={g.rows}
              collapsed={!!collapsed[g.key]}
              onToggle={() => setCollapsed((c) => ({ ...c, [g.key]: !c[g.key] }))}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function GroupBlock({ groupBy, label, rows, collapsed, onToggle }: {
  groupBy: GroupBy;
  label: string;
  rows: ContainerView[];
  collapsed: boolean;
  onToggle: () => void;
}) {
  const showNode = groupBy !== 'node';
  const problems = rows.filter((c) => c.crashed_loop).length;

  return (
    <section className="card overflow-hidden p-0">
      {groupBy !== 'none' && (
        <button
          onClick={onToggle}
          aria-expanded={!collapsed}
          className={`flex w-full items-center gap-2 px-4 py-3 text-left transition hover:bg-zinc-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent dark:hover:bg-zinc-800/40 ${collapsed ? '' : 'border-b border-zinc-200 dark:border-zinc-800'}`}
        >
          {collapsed ? <ChevronRight className="h-4 w-4 text-zinc-400" /> : <ChevronDown className="h-4 w-4 text-zinc-400" />}
          {groupBy === 'node' ? <Server className="h-4 w-4 text-zinc-500" /> : <Layers className="h-4 w-4 text-zinc-500" />}
          <h2 className="text-base font-semibold tracking-tight text-zinc-900 dark:text-zinc-100">{label}</h2>
          <span className="badge-zinc">{rows.length}</span>
          {problems > 0 && <span className="badge-red">{problems} crashed</span>}
        </button>
      )}
      {!collapsed && (
        <div className="overflow-x-auto">
          {/* Fixed layout with shared widths keeps the columns aligned from one block to the next. */}
          <table className="w-full min-w-[48rem] table-fixed text-sm">
            <colgroup>
              <col className="w-[22%]" />
              <col className="w-[26%]" />
              {showNode && <col className="w-[12%]" />}
              <col className="w-[11%]" />
              <col className="w-[7%]" />
              <col className="w-[12%]" />
              <col className="w-[10%]" />
              <col className="w-[8%]" />
            </colgroup>
            <thead className="border-b border-zinc-100 dark:border-zinc-800/80">
              <tr>
                <th className="th">Container</th>
                <th className="th">Image</th>
                {showNode && <th className="th">Node</th>}
                <th className="th">State</th>
                <th className="th text-right">CPU</th>
                <th className="th text-right">RAM</th>
                <th className="th text-right">Network</th>
                <th className="th text-right">Uptime</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800/80">
              {rows.map((c) => (
                <ContainerRow key={c.node_id + c.container_id} c={c} showNode={showNode} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
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

function ContainerRow({ c, showNode }: { c: ContainerView; showNode: boolean }) {
  const router = useRouter();
  const img = parseImage(c.image);
  const href = `/container?node=${encodeURIComponent(c.node_id)}&id=${encodeURIComponent(c.container_id)}`;

  const badgeCls =
    c.crashed_loop ? 'badge-red' :
    c.state === 'running' && c.health === 'unhealthy' ? 'badge-amber' :
    c.state === 'running' ? 'badge-green' :
    c.state === 'restarting' ? 'badge-amber' :
    'badge-zinc';
  const badgeLabel = c.crashed_loop ? `crashed ×${c.recent_restarts}` : (c.health || c.state || 'unknown');

  return (
    <tr className="row-link" onClick={() => router.push(href)}>
      <td className="td max-w-[18rem]">
        <Link
          href={href}
          onClick={(e) => e.stopPropagation()}
          className="block truncate font-medium hover:text-accent focus:outline-none focus-visible:underline"
        >
          {c.name}
        </Link>
      </td>

      <td className="td max-w-[20rem]">
        <div className="flex items-baseline gap-1.5">
          <span className="truncate font-mono text-xs" title={c.image}>
            {img.namespace && <span className="text-zinc-400">{img.namespace}/</span>}
            {img.name || '-'}
          </span>
          {img.tag ? (
            <span className="badge-accent shrink-0">{img.tag}</span>
          ) : img.pinned ? (
            <Tooltip label="Pinned by digest"><span className="badge-zinc shrink-0 font-mono">digest</span></Tooltip>
          ) : (
            <span className="badge-zinc shrink-0 font-mono">latest</span>
          )}
        </div>
      </td>

      {showNode && <td className="td truncate font-mono text-2xs text-zinc-500">{c.node_id}</td>}

      <td className="td"><span className={badgeCls}>{badgeLabel}</span></td>

      <td className="td text-right font-mono text-xs">{fmtPct(c.cpu_percent, 1)}</td>

      <td className="td text-right font-mono text-xs">
        {fmtBytes(c.mem_used_bytes)}
        {c.mem_limit_bytes > 0 && <span className="text-zinc-400"> / {fmtBytes(c.mem_limit_bytes)}</span>}
      </td>

      <td className="td whitespace-nowrap text-right font-mono text-2xs text-zinc-500">
        <div>↓ {fmtBps(c.net_rx_bps)}</div>
        <div>↑ {fmtBps(c.net_tx_bps)}</div>
      </td>

      <td className="td text-right text-2xs text-zinc-500">
        <div>{c.state === 'running' ? fmtRelative(c.started_at_ms) : c.state}</div>
        {c.restart_count > 0 && (
          <Tooltip label={`${c.restart_count} restarts total`}>
            <span className="inline-flex items-center gap-1">
              <RotateCcw className="h-3 w-3" aria-hidden />{c.restart_count}
              <span className="sr-only">restarts</span>
            </span>
          </Tooltip>
        )}
      </td>
    </tr>
  );
}
