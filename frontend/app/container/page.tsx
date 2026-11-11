'use client';

import { Suspense, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import Link from 'next/link';
import { api, errText } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import type { ContainerView } from '@/lib/types';
import { fmtBytes, fmtBps, fmtPct, fmtAbsTime, fmtRelative } from '@/lib/format';
import { Bar } from '@/components/bar';
import { AuthShell } from '@/components/auth-shell';
import { LogViewer } from '@/components/log-viewer';
import { BlockSkeleton, ErrorState } from '@/components/feedback';

export default function ContainerPage() {
  return (
    <AuthShell>
      <Suspense fallback={<div className="page"><BlockSkeleton /></div>}>
        <Detail />
      </Suspense>
    </AuthShell>
  );
}

function Detail() {
  const params = useSearchParams();
  const node = params.get('node') ?? '';
  const id = params.get('id') ?? '';
  const [c, setC] = useState<ContainerView | null>(null);
  const [tab, setTab] = useState<'overview' | 'logs'>('overview');
  const [err, setErr] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let alive = true;
    const load = async () => {
      try {
        const v = await api<ContainerView>(`/api/containers/${encodeURIComponent(node)}/${encodeURIComponent(id)}`);
        if (alive) { setC(v); setErr(null); }
      } catch (e) {
        if (alive) setErr((e as { status?: number })?.status === 404
          ? 'This container is not reported by its node any more. It may have been removed.'
          : errText(e, 'Could not load this container.'));
      }
    };
    load();
    const t = setInterval(load, 10_000);
    return () => { alive = false; clearInterval(t); };
  }, [node, id, attempt]);

  useSSE({
    'containers.update': (data: unknown) => {
      const u = data as { node_id: string; containers: ContainerView[] };
      if (u.node_id !== node) return;
      const updated = (u.containers ?? []).find((x) => x.container_id === id);
      if (updated) setC(updated);
    },
  });

  if (!c) {
    return (
      <div className="page">
        {err ? (
          <>
            <ErrorState message={err} onRetry={() => setAttempt((a) => a + 1)} />
            <Link href="/containers" className="btn-ghost">Back to Containers</Link>
          </>
        ) : <BlockSkeleton />}
      </div>
    );
  }

  const stateBadge =
    c.crashed_loop ? 'badge-red' :
    c.state === 'running' && c.health === 'unhealthy' ? 'badge-amber' :
    c.state === 'running' ? 'badge-green' :
    c.state === 'restarting' ? 'badge-amber' :
    'badge-zinc';

  return (
    <div className="page">
      {err && <ErrorState message={err} onRetry={() => setAttempt((a) => a + 1)} />}
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h1 className="page-title break-words">{c.name}</h1>
          <p className="page-sub break-all">{c.image} · on <span className="font-mono">{c.node_id}</span></p>
        </div>
        <span className={stateBadge}>{c.crashed_loop ? `crashed ×${c.recent_restarts}/60s` : (c.health || c.state)}</span>
      </header>

      <div className="seg mb-4" role="tablist" aria-label="Container view">
        {(['overview','logs'] as const).map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={`${tab === t ? 'seg-item-active' : 'seg-item'} capitalize`}
          >{t}</button>
        ))}
      </div>

      {tab === 'overview' ? (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2 3xl:grid-cols-3">
          <div className="card space-y-4">
            <div className="section-title">Resources</div>
            <Bar label="CPU" value={c.cpu_percent} detail={fmtPct(c.cpu_percent, 1)} />
            <Bar label="RAM" value={c.mem_percent} detail={`${fmtBytes(c.mem_used_bytes)}${c.mem_limit_bytes ? ` / ${fmtBytes(c.mem_limit_bytes)}` : ''}`} tone={c.mem_limit_bytes ? 'auto' : 'cool'} />
            <div className="grid grid-cols-2 gap-x-4 gap-y-2 pt-2 text-sm text-zinc-500">
              <div>↓ Net <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBps(c.net_rx_bps)}</span></div>
              <div className="text-right">↑ Net <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBps(c.net_tx_bps)}</span></div>
              <div>Net total ↓ <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBytes(c.net_rx_bytes)}</span></div>
              <div className="text-right">Net total ↑ <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBytes(c.net_tx_bytes)}</span></div>
              <div>Block read <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBytes(c.block_read_bytes)}</span></div>
              <div className="text-right">Block write <span className="font-mono text-zinc-700 dark:text-zinc-300">{fmtBytes(c.block_write_bytes)}</span></div>
            </div>
          </div>

          <div className="card space-y-2.5 text-sm">
            <div className="section-title">State</div>
            <Row k="State" v={c.state} />
            <Row k="Health" v={c.health || '-'} />
            <Row k="Restart count" v={String(c.restart_count)} />
            <Row k="Recent restarts (60s)" v={String(c.recent_restarts)} />
            <Row k="Exit code" v={String(c.exit_code)} />
            <Row k="Started" v={fmtAbsTime(c.started_at_ms)} />
            <Row k="Finished" v={c.finished_at_ms > 0 ? fmtAbsTime(c.finished_at_ms) : '-'} />
            <Row k="Reported" v={fmtRelative(c.reported_at)} />
          </div>

          {Object.keys(c.labels || {}).length > 0 && (
            <div className="card lg:col-span-2 3xl:col-span-1">
              <div className="section-title mb-3">Labels</div>
              <div className="grid gap-1.5 break-all font-mono text-xs">
                {Object.entries(c.labels).map(([k, v]) => (
                  <div key={k}><span className="text-zinc-500">{k}</span> = <span>{v}</span></div>
                ))}
              </div>
            </div>
          )}
        </div>
      ) : (
        <div className="card">
          <LogViewer nodeID={c.node_id} containerID={c.container_id} />
        </div>
      )}
    </div>
  );
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex justify-between">
      <span className="text-zinc-500">{k}</span>
      <span className="font-mono text-zinc-700 dark:text-zinc-300">{v}</span>
    </div>
  );
}
