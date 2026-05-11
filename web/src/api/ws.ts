import type { HealthMap } from './types';

export type WSMessage =
  | { type: 'hc_update'; data: HealthMap }
  | { type: string; data: unknown };

// connectHC opens /api/ws, reconnects with exponential backoff (capped at
// 30s), and invokes onMessage for each envelope. The returned function
// fully tears down the connection.
export function connectHC(
  onMessage: (msg: WSMessage) => void,
  onState?: (state: 'open' | 'closed') => void,
): () => void {
  let closed = false;
  let ws: WebSocket | null = null;
  let backoff = 1000;

  const open = () => {
    if (closed) return;
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${proto}//${window.location.host}/api/ws`;
    ws = new WebSocket(url);

    ws.onopen = () => {
      backoff = 1000;
      onState?.('open');
    };
    ws.onmessage = (e) => {
      try {
        onMessage(JSON.parse(e.data) as WSMessage);
      } catch {
        /* ignore non-JSON frames */
      }
    };
    ws.onerror = () => {
      /* let onclose handle reconnect */
    };
    ws.onclose = () => {
      onState?.('closed');
      if (closed) return;
      const delay = Math.min(backoff, 30_000);
      backoff = Math.min(backoff * 2, 30_000);
      setTimeout(open, delay);
    };
  };

  open();
  return () => {
    closed = true;
    ws?.close();
  };
}
