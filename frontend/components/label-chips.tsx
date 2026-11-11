'use client';

import { useState } from 'react';

/**
 * LabelChips renders a node's labels compactly enough for a table row.
 *
 * Two sources, deliberately told apart: `swarm` comes from the Docker API
 * (`docker node inspect` → Spec.Labels), `agent` comes from BPM_AGENT_LABELS in
 * the agent's own environment. They look alike but mean different things, and
 * only the swarm ones drive placement constraints.
 */
export function LabelChips({
  swarm,
  agent,
  max = 3,
}: {
  swarm?: Record<string, string> | null;
  agent?: Record<string, string> | null;
  max?: number;
}) {
  const [expanded, setExpanded] = useState(false);

  const items = [
    ...Object.entries(swarm ?? {}).map(([k, v]) => ({ k, v, source: 'swarm' as const })),
    ...Object.entries(agent ?? {}).map(([k, v]) => ({ k, v, source: 'agent' as const })),
  ].sort((a, b) => a.k.localeCompare(b.k));

  if (items.length === 0) {
    return <span className="text-2xs text-zinc-400 dark:text-zinc-600">-</span>;
  }

  const shown = expanded ? items : items.slice(0, max);
  const hidden = items.length - shown.length;

  return (
    <div className="flex flex-wrap items-center gap-1">
      {shown.map(({ k, v, source }) => (
        <span
          key={source + k}
          title={`${source} label · ${k}=${v}`}
          className={
            source === 'swarm'
              ? 'badge max-w-[13rem] truncate border border-accent/25 bg-accent/10 font-mono text-accent'
              : 'badge-outline max-w-[13rem] truncate'
          }
        >
          {k}
          {v ? <span className="opacity-60">={v}</span> : null}
        </span>
      ))}
      {hidden > 0 && (
        <button
          onClick={(e) => { e.stopPropagation(); setExpanded(true); }}
          className="rounded px-1 text-2xs text-zinc-500 underline-offset-2 transition hover:text-zinc-800 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:hover:text-zinc-200"
        >
          +{hidden}
        </button>
      )}
      {expanded && items.length > max && (
        <button
          onClick={(e) => { e.stopPropagation(); setExpanded(false); }}
          className="rounded px-1 text-2xs text-zinc-500 underline-offset-2 transition hover:text-zinc-800 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:hover:text-zinc-200"
        >
          less
        </button>
      )}
    </div>
  );
}
