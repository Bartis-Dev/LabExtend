// useSSE — React hook around EventSource with auto-reconnect.
// Caller passes a topic→handler map; the hook subscribes once and routes
// inbound events.
//
// TODO(phase 5): expose <SseProvider> that opens a single shared connection,
// plus useSseTopic('backup.progress', cb) consumers — avoids one EventSource
// per component.

'use client';

import { useEffect, useRef } from 'react';

export type SseHandlers = Record<string, (data: unknown) => void>;

export interface UseSSEOptions {
  /** Path to the SSE endpoint, default "/api/events". */
  url?: string;
  /** Initial reconnect delay (ms). Doubles up to max on each failure. */
  initialBackoffMs?: number;
  /** Max reconnect delay (ms). */
  maxBackoffMs?: number;
}

export function useSSE(handlers: SseHandlers, opts: UseSSEOptions = {}) {
  const url = opts.url ?? '/api/events';
  const initial = opts.initialBackoffMs ?? 500;
  const max = opts.maxBackoffMs ?? 30_000;

  const handlersRef = useRef(handlers);
  handlersRef.current = handlers;

  useEffect(() => {
    let es: EventSource | null = null;
    let cancelled = false;
    let backoff = initial;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const connect = () => {
      if (cancelled) return;
      es = new EventSource(url, { withCredentials: true });

      es.onopen = () => {
        backoff = initial; // reset on success
      };

      es.onerror = () => {
        es?.close();
        es = null;
        if (cancelled) return;
        timer = setTimeout(connect, backoff);
        backoff = Math.min(backoff * 2, max);
      };

      // Register a listener per topic.
      for (const topic of Object.keys(handlersRef.current)) {
        es.addEventListener(topic, (ev: MessageEvent) => {
          try {
            const data = JSON.parse(ev.data);
            handlersRef.current[topic]?.(data);
          } catch {
            handlersRef.current[topic]?.(ev.data);
          }
        });
      }
    };

    connect();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
      es?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, initial, max, Object.keys(handlers).join(',')]);
}
