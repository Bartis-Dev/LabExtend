'use client';

import { Suspense, useCallback, useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import Link from 'next/link';
import { ArrowLeft, RotateCw, Power } from 'lucide-react';
import { AuthShell } from '@/components/auth-shell';
import '@xterm/xterm/css/xterm.css';

export default function TerminalPage() {
  return (
    <AuthShell>
      <Suspense fallback={<div className="p-8 text-sm text-zinc-500">Loading…</div>}>
        <NodeTerminal />
      </Suspense>
    </AuthShell>
  );
}

/** What the leader sends on the text channel; terminal bytes come as binary. */
interface ServerMsg {
  type: 'ready' | 'closed' | 'error';
  session_id?: string;
  container_id?: string;
  image?: string;
  max_seconds?: number;
  reason?: string;
  error?: string;
}

type Status =
  | { kind: 'connecting' }
  | { kind: 'live'; containerID: string; since: number; maxSeconds: number }
  | { kind: 'closed'; reason: string }
  | { kind: 'error'; message: string };

function NodeTerminal() {
  const params = useSearchParams();
  const nodeID = params.get('node') ?? '';

  const hostRef = useRef<HTMLDivElement | null>(null);
  const [status, setStatus] = useState<Status>({ kind: 'connecting' });
  // Bumping this remounts the whole session — simpler and more predictable
  // than reusing a terminal whose scrollback belongs to a dead shell.
  const [attempt, setAttempt] = useState(0);
  const closeRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    if (!nodeID || !hostRef.current) return;
    const host = hostRef.current;
    let disposed = false;
    let cleanup = () => {};

    // xterm touches `window` at import time, so it cannot be part of the
    // static export's prerender.
    (async () => {
      const [{ Terminal }, { FitAddon }] = await Promise.all([
        import('@xterm/xterm'),
        import('@xterm/addon-fit'),
      ]);
      if (disposed) return;

      const dark = window.matchMedia('(prefers-color-scheme: dark)').matches;
      const term = new Terminal({
        allowProposedApi: true,
        cursorBlink: true,
        fontFamily:
          'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
        fontSize: 13,
        lineHeight: 1.25,
        scrollback: 10_000,
        theme: dark
          ? { background: '#18181b', foreground: '#e4e4e7', cursor: '#60a5fa', selectionBackground: '#3f3f46' }
          : { background: '#ffffff', foreground: '#27272a', cursor: '#2563eb', selectionBackground: '#dbeafe' },
      });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(host);
      fit.fit();
      term.focus();

      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const url =
        `${proto}//${window.location.host}/api/nodes/${encodeURIComponent(nodeID)}/terminal` +
        `?cols=${term.cols}&rows=${term.rows}`;
      const ws = new WebSocket(url);
      ws.binaryType = 'arraybuffer';

      const decoder = new TextDecoder();
      const encoder = new TextEncoder();

      ws.onmessage = (ev) => {
        if (typeof ev.data === 'string') {
          let msg: ServerMsg;
          try {
            msg = JSON.parse(ev.data) as ServerMsg;
          } catch {
            return;
          }
          if (msg.type === 'ready') {
            setStatus({
              kind: 'live',
              containerID: msg.container_id ?? '',
              since: Date.now(),
              maxSeconds: msg.max_seconds ?? 3600,
            });
          } else if (msg.type === 'closed') {
            setStatus({ kind: 'closed', reason: msg.reason || 'session ended' });
          } else if (msg.type === 'error') {
            setStatus({ kind: 'error', message: msg.error || 'could not open a shell' });
          }
          return;
        }
        term.write(decoder.decode(new Uint8Array(ev.data as ArrayBuffer)));
      };

      ws.onerror = () => {
        setStatus((s) => (s.kind === 'live' ? s : { kind: 'error', message: 'connection failed' }));
      };
      ws.onclose = () => {
        setStatus((s) =>
          s.kind === 'closed' || s.kind === 'error' ? s : { kind: 'closed', reason: 'disconnected' },
        );
      };

      const onData = term.onData((data) => {
        if (ws.readyState === WebSocket.OPEN) ws.send(encoder.encode(data));
      });

      const sendResize = () => {
        if (ws.readyState !== WebSocket.OPEN) return;
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
      };
      const onResize = term.onResize(sendResize);

      // ResizeObserver rather than window.resize: the sidebar and the status
      // bar can change the terminal's box without the window moving at all.
      const ro = new ResizeObserver(() => {
        try {
          fit.fit();
        } catch {
          /* element not laid out yet */
        }
      });
      ro.observe(host);

      closeRef.current = () => ws.close(1000, 'closed by user');

      cleanup = () => {
        ro.disconnect();
        onData.dispose();
        onResize.dispose();
        closeRef.current = null;
        ws.close(1000, 'navigated away');
        term.dispose();
      };
    })();

    return () => {
      disposed = true;
      cleanup();
    };
  }, [nodeID, attempt]);

  const reconnect = useCallback(() => {
    setStatus({ kind: 'connecting' });
    setAttempt((n) => n + 1);
  }, []);

  if (!nodeID) {
    return <div className="p-8 text-sm text-zinc-500">No node selected.</div>;
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center gap-3 border-b border-zinc-200 px-4 py-2.5 dark:border-zinc-800">
        <Link
          href={`/node?id=${encodeURIComponent(nodeID)}`}
          className="btn-ghost -ml-2 h-8 px-2 text-xs"
        >
          <ArrowLeft className="h-3.5 w-3.5" /> {nodeID}
        </Link>

        <span className="text-xs text-zinc-500">root shell on the host</span>

        <div className="ml-auto flex items-center gap-2">
          <StatusPill status={status} />
          {status.kind === 'live' ? (
            <button onClick={() => closeRef.current?.()} className="btn-ghost h-8 px-2 text-xs">
              <Power className="h-3.5 w-3.5" /> end session
            </button>
          ) : (
            <button onClick={reconnect} className="btn-ghost h-8 px-2 text-xs">
              <RotateCw className="h-3.5 w-3.5" /> reconnect
            </button>
          )}
        </div>
      </header>

      {status.kind === 'error' && (
        <p className="border-b border-red-200 bg-red-50 px-4 py-2 text-xs text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
          {status.message}
        </p>
      )}

      <div className="min-h-0 flex-1 bg-white p-2 dark:bg-zinc-900">
        <div ref={hostRef} className="h-full w-full" />
      </div>
    </div>
  );
}

function StatusPill({ status }: { status: Status }) {
  const [now, setNow] = useState(() => Date.now());
  const live = status.kind === 'live';

  useEffect(() => {
    if (!live) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [live]);

  if (status.kind === 'connecting') return <span className="badge-zinc">connecting…</span>;
  if (status.kind === 'error') return <span className="badge-red">failed</span>;
  if (status.kind === 'closed') return <span className="badge-zinc">{status.reason}</span>;

  const elapsed = Math.floor((now - status.since) / 1000);
  const left = Math.max(0, status.maxSeconds - elapsed);
  return (
    <span className="badge-green" title={status.containerID ? `container ${status.containerID.slice(0, 12)}` : ''}>
      live · {fmtClock(left)} left
    </span>
  );
}

function fmtClock(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${String(s).padStart(2, '0')}`;
}
