'use client';

import { useEffect, useRef, useState } from 'react';
import clsx from 'clsx';
import { Download, Eraser, Pause, Play } from 'lucide-react';
import type { LogEntry } from '@/lib/types';

interface Props {
  nodeID: string;
  containerID: string;
}

/**
 * LogViewer — connects to /api/containers/{node}/{id}/logs/stream (SSE).
 * Shows persisted tail first, then live lines. Pause toggles new render.
 */
export function LogViewer({ nodeID, containerID }: Props) {
  const [lines, setLines] = useState<LogEntry[]>([]);
  const [paused, setPaused] = useState(false);
  const [search, setSearch] = useState('');
  const [connected, setConnected] = useState(false);
  const bufferRef = useRef<LogEntry[]>([]);
  const containerRef = useRef<HTMLDivElement>(null);
  const followRef = useRef(true);

  useEffect(() => {
    if (!nodeID || !containerID) return;
    const url = `/api/containers/${encodeURIComponent(nodeID)}/${encodeURIComponent(containerID)}/logs/stream`;
    const es = new EventSource(url, { withCredentials: true });

    es.addEventListener('ready', () => setConnected(true));
    es.addEventListener('log', (ev: MessageEvent) => {
      try {
        const e = JSON.parse(ev.data) as LogEntry;
        bufferRef.current.push(e);
        if (!paused) {
          setLines((prev) => {
            const next = prev.concat(bufferRef.current);
            bufferRef.current = [];
            return next.length > 5000 ? next.slice(-5000) : next;
          });
        }
      } catch {/* */}
    });
    es.onerror = () => setConnected(false);

    return () => {
      es.close();
    };
  }, [nodeID, containerID, paused]);

  // Auto-scroll while in "follow" mode (user hasn't scrolled away).
  useEffect(() => {
    if (!followRef.current || !containerRef.current) return;
    containerRef.current.scrollTop = containerRef.current.scrollHeight;
  }, [lines]);

  const onScroll = () => {
    if (!containerRef.current) return;
    const el = containerRef.current;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    followRef.current = atBottom;
  };

  const filtered = search
    ? lines.filter((l) => l.line.toLowerCase().includes(search.toLowerCase()))
    : lines;

  return (
    <div className="flex h-[60vh] flex-col">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <input
          placeholder="filter…"
          aria-label="Filter log lines"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="input w-56"
        />
        <button
          onClick={() => {
            setPaused((p) => {
              if (p && bufferRef.current.length > 0) {
                setLines((prev) => prev.concat(bufferRef.current).slice(-5000));
                bufferRef.current = [];
              }
              return !p;
            });
          }}
          className={paused ? 'btn-primary' : 'btn-ghost'}
        >
          {paused
            ? <><Play className="h-4 w-4" /> Resume ({bufferRef.current.length})</>
            : <><Pause className="h-4 w-4" /> Pause</>}
        </button>
        <button
          onClick={() => setLines([])}
          className="btn-ghost"
        ><Eraser className="h-4 w-4" /> Clear</button>
        <button
          onClick={() => {
            const blob = new Blob(filtered.map((l) => l.line + '\n'), { type: 'text/plain' });
            const a = document.createElement('a');
            a.href = URL.createObjectURL(blob);
            a.download = `${containerID.slice(0, 12)}.log`;
            a.click();
          }}
          className="btn-ghost"
        ><Download className="h-4 w-4" /> Download</button>
        <div className="ml-auto text-2xs text-zinc-500">
          {connected ? <span className="text-emerald-700 dark:text-emerald-400">● live</span> : <span className="text-amber-700 dark:text-amber-400">● connecting…</span>}
          {' · '}{filtered.length} lines
        </div>
      </div>
      <div
        ref={containerRef}
        onScroll={onScroll}
        className="flex-1 overflow-auto rounded-md border border-zinc-200 bg-zinc-950 p-3 font-mono text-xs leading-snug text-zinc-200 dark:border-zinc-800"
      >
        {filtered.map((l) => (
          <div key={l.id || `${l.ts_ms}-${Math.random()}`} className={clsx('whitespace-pre-wrap', l.stream === 'stderr' && 'text-red-400')}>
            <span className="mr-2 select-none text-zinc-600">{new Date(l.ts_ms).toLocaleTimeString()}</span>
            {l.line}
          </div>
        ))}
        {filtered.length === 0 && (
          <div className="text-zinc-500">No log lines.</div>
        )}
      </div>
    </div>
  );
}
