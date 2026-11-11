'use client';

import Link from 'next/link';
import { useCallback, useEffect, useRef, useState } from 'react';
import clsx from 'clsx';
import { Bell, CheckCheck, ArrowUpRight, TriangleAlert } from 'lucide-react';
import { api } from '@/lib/api';
import { useSSE } from '@/lib/sse';
import { fmtRelative } from '@/lib/format';
import type { Notification } from '@/lib/types';
import { toast } from './toast';

const DOT: Record<Notification['severity'], string> = {
  critical: 'bg-red-500',
  warning: 'bg-amber-500',
  info: 'bg-zinc-400 dark:bg-zinc-500',
};

/**
 * NotificationBell is the notification center: unread count on the bell, a
 * panel with the recent events, and a toast for every new critical one, so a
 * failed backup is seen even when the panel stays closed.
 */
export function NotificationBell() {
  const [items, setItems] = useState<Notification[]>([]);
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const root = useRef<HTMLDivElement>(null);

  const load = useCallback(async () => {
    try {
      const r = await api<{ notifications: Notification[]; unread: number }>('/api/notifications', { query: { limit: 50 } });
      setItems(r.notifications ?? []);
      setUnread(r.unread ?? 0);
      setLoadErr(null);
    } catch {
      setLoadErr('Could not load notifications. They reload when the connection returns.');
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  useSSE({
    ready: () => load(), // catches up on events missed while disconnected
    notification: (d) => {
      const n = d as Notification;
      setItems((prev) => [n, ...prev.filter((p) => p.id !== n.id)].slice(0, 50));
      setUnread((u) => u + 1);
      if (n.severity === 'critical') toast.error(n.title, 8000);
    },
    'notification.updated': (d) => {
      const n = d as Notification;
      setItems((prev) => prev.map((p) => (p.id === n.id ? { ...p, delivery_error: n.delivery_error } : p)));
    },
    'notification.read': () => load(),
  });

  // Close on outside click and Escape.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false); };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const markRead = async (n: Notification) => {
    if (n.read_at) return;
    const now = Math.floor(Date.now() / 1000);
    setItems((prev) => prev.map((p) => (p.id === n.id ? { ...p, read_at: now } : p)));
    setUnread((u) => Math.max(0, u - 1));
    try { await api(`/api/notifications/${n.id}/read`, { method: 'POST' }); } catch { load(); }
  };

  const markAll = async () => {
    try {
      await api('/api/notifications/read-all', { method: 'POST' });
      const now = Math.floor(Date.now() / 1000);
      setItems((prev) => prev.map((p) => ({ ...p, read_at: p.read_at ?? now })));
      setUnread(0);
    } catch {
      setLoadErr('Marking all as read failed. Try again.');
    }
  };

  const hasCritical = items.some((n) => !n.read_at && n.severity === 'critical');

  return (
    <div ref={root} className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-label={unread ? `Notifications, ${unread} unread` : 'Notifications'}
        aria-expanded={open}
        aria-haspopup="dialog"
        className={clsx(
          'relative rounded-md p-1.5 text-zinc-600 transition dark:text-zinc-400 hover:bg-zinc-100 hover:text-zinc-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent active:bg-zinc-200 dark:hover:bg-zinc-800 dark:hover:text-zinc-100 dark:active:bg-zinc-700',
          open && 'bg-zinc-100 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100',
        )}
      >
        <Bell className="h-4 w-4" />
        {unread > 0 && (
          <span
            className={clsx(
              'absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-3xs font-semibold leading-none text-white',
              hasCritical ? 'bg-red-500' : 'bg-accent',
            )}
          >
            {unread > 99 ? '99+' : unread}
          </span>
        )}
      </button>

      {open && (
        <div
          role="dialog"
          aria-label="Notifications"
          className="modal-panel fixed left-3 right-3 top-14 z-50 flex max-h-[min(36rem,calc(100vh-5rem))] flex-col overflow-hidden rounded-xl border border-zinc-200 bg-white shadow-xl dark:border-zinc-800 dark:bg-zinc-900 sm:absolute sm:left-0 sm:right-auto sm:top-9 sm:w-[24rem]"
        >
          <header className="flex items-center justify-between border-b border-zinc-200 px-4 py-2.5 dark:border-zinc-800">
            <div className="text-sm font-semibold">
              Notifications
              {unread > 0 && <span className="ml-2 text-xs font-normal text-zinc-500">{unread} unread</span>}
            </div>
            <button type="button" onClick={markAll} disabled={unread === 0} className="btn-ghost h-7 px-2 text-xs">
              <CheckCheck className="h-3.5 w-3.5" /> Mark all read
            </button>
          </header>

          {loadErr && (
            <div role="alert" className="border-b border-red-200 bg-red-50 px-4 py-2 text-xs text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300">
              {loadErr}
            </div>
          )}

          <ul className="flex-1 overflow-y-auto">
            {items.map((n) => (
              <li
                key={n.id}
                className={clsx(
                  'border-b border-zinc-100 px-4 py-3 last:border-0 dark:border-zinc-800',
                  !n.read_at && 'bg-zinc-50 dark:bg-zinc-800/40',
                )}
              >
                <div className="flex items-start gap-2.5">
                  <span className={clsx('mt-1.5 h-2 w-2 shrink-0 rounded-full', DOT[n.severity])} aria-label={n.severity} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-baseline justify-between gap-2">
                      <span className={clsx('truncate text-sm', !n.read_at ? 'font-semibold' : 'font-medium text-zinc-700 dark:text-zinc-300')}>
                        {n.title}
                      </span>
                      <time className="shrink-0 text-2xs text-zinc-500" dateTime={new Date(n.ts * 1000).toISOString()} title={new Date(n.ts * 1000).toLocaleString()}>
                        {fmtRelative(n.ts)}
                      </time>
                    </div>
                    {n.body && (
                      <p className="mt-0.5 line-clamp-4 whitespace-pre-line break-words text-xs text-zinc-600 dark:text-zinc-400">{n.body}</p>
                    )}
                    {n.delivery_error && (
                      <p className="mt-1.5 flex items-start gap-1 text-2xs text-red-600 dark:text-red-400">
                        <TriangleAlert className="mt-px h-3 w-3 shrink-0" />
                        <span className="break-words">Webhook delivery failed: {n.delivery_error}</span>
                      </p>
                    )}
                    <div className="mt-1.5 flex items-center gap-3 text-xs">
                      {n.link && (
                        <Link
                          href={n.link}
                          onClick={() => { markRead(n); setOpen(false); }}
                          className="inline-flex items-center gap-0.5 rounded font-medium text-accent hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                        >
                          Open <ArrowUpRight className="h-3 w-3" />
                        </Link>
                      )}
                      {!n.read_at && (
                        <button
                          type="button"
                          onClick={() => markRead(n)}
                          className="rounded text-zinc-500 hover:text-zinc-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:hover:text-zinc-100"
                        >
                          Mark read
                        </button>
                      )}
                    </div>
                  </div>
                </div>
              </li>
            ))}
            {items.length === 0 && !loadErr && (
              <li className="px-4 py-10 text-center">
                <Bell className="mx-auto mb-2 h-5 w-5 text-zinc-300 dark:text-zinc-600" />
                <div className="text-sm font-medium">No notifications</div>
                <div className="mt-0.5 text-xs text-zinc-500">Failed backups, offline nodes and crash loops show up here.</div>
              </li>
            )}
          </ul>
        </div>
      )}

    </div>
  );
}
