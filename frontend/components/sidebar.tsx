'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useEffect, useRef, useState } from 'react';
import clsx from 'clsx';
import {
  Server, Boxes, Bell, Webhook, FolderOpen, Clock,
  HardDrive, Database, LogOut, Users, ScrollText, UserCircle, RefreshCw,
} from 'lucide-react';
import { api, setCSRFToken, type ApiError } from '@/lib/api';

const NAV = [
  { href: '/nodes',      label: 'Nodes',     icon: Server },
  { href: '/containers', label: 'Containers',icon: Boxes },
  { href: '/alerts',     label: 'Alerts',    icon: Bell },
  { href: '/webhooks',   label: 'Webhooks',  icon: Webhook },
  { href: '/files',      label: 'Files',     icon: FolderOpen },
  { href: '/cronjobs',   label: 'Cron',      icon: Clock },
  { href: '/s3',         label: 'S3',        icon: HardDrive },
  { href: '/backups',    label: 'Backups',   icon: Database },
  { href: '/users',      label: 'Users',     icon: Users },
  { href: '/audit',      label: 'Audit',     icon: ScrollText },
  { href: '/account',    label: 'Account',   icon: UserCircle },
];

export function Sidebar({ userEmail }: { userEmail?: string }) {
  const pathname = usePathname();
  return (
    <aside className="flex h-screen w-60 flex-col border-r border-zinc-200 bg-white px-3 py-4 dark:border-zinc-800 dark:bg-zinc-950">
      <div className="px-2 pb-4">
        <div className="text-sm font-semibold tracking-tight">LabExtend</div>
        <div className="text-[11px] text-zinc-500">{userEmail ?? ''}</div>
      </div>

      <nav className="flex-1 space-y-0.5">
        {NAV.map((item) => {
          const Icon = item.icon;
          const active = pathname === item.href || pathname.startsWith(item.href + '/');
          return (
            <Link
              key={item.href}
              href={item.href}
              className={clsx(
                'group flex items-center justify-between rounded-md px-2.5 py-1.5 text-sm transition',
                active
                  ? 'bg-zinc-100 font-medium text-zinc-900 dark:bg-zinc-800 dark:text-white'
                  : 'text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-900 dark:hover:text-zinc-100'
              )}
            >
              <span className="inline-flex items-center gap-2.5">
                <Icon className="h-4 w-4" />
                {item.label}
              </span>
            </Link>
          );
        })}
      </nav>

      <RestartPortainerButton />

      <button
        type="button"
        onClick={async () => {
          try {
            await api('/api/auth/logout', { method: 'POST' });
          } catch {/* ignore */}
          setCSRFToken(null);
          window.location.href = '/login';
        }}
        className="mt-1 inline-flex items-center gap-2 rounded-md px-2.5 py-1.5 text-sm text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900 dark:hover:bg-zinc-900 dark:hover:text-zinc-100"
      >
        <LogOut className="h-4 w-4" /> Sign out
      </button>
    </aside>
  );
}

// 60s cooldown after a successful restart, persisted so a page reload keeps the
// button greyed out (the swarm redeploy itself takes a moment to settle).
const RESTART_COOLDOWN_MS = 60_000;
const RESTART_COOLDOWN_KEY = 'portainer-restart-cooldown-until';

// RestartPortainerButton force-redeploys the Portainer agent service via the
// leader's manager socket (POST /api/services/portainer-agent/restart). Fixes
// Portainer's recurring "no agent on environment" without leaving the UI.
// Guarded by a confirm dialog and a per-browser cooldown.
function RestartPortainerButton() {
  const [busy, setBusy] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [note, setNote] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null);
  const [cooldownUntil, setCooldownUntil] = useState(0);
  const [remaining, setRemaining] = useState(0);
  const confirmRef = useRef<HTMLButtonElement>(null);

  // Restore an in-flight cooldown across reloads.
  useEffect(() => {
    const raw = window.localStorage.getItem(RESTART_COOLDOWN_KEY);
    const until = raw ? Number(raw) : 0;
    if (until > Date.now()) setCooldownUntil(until);
    else if (raw) window.localStorage.removeItem(RESTART_COOLDOWN_KEY);
  }, []);

  // Tick the remaining-seconds counter while a cooldown is active.
  useEffect(() => {
    if (!cooldownUntil) {
      setRemaining(0);
      return;
    }
    const tick = () => {
      const ms = cooldownUntil - Date.now();
      if (ms <= 0) {
        setRemaining(0);
        setCooldownUntil(0);
        window.localStorage.removeItem(RESTART_COOLDOWN_KEY);
      } else {
        setRemaining(Math.ceil(ms / 1000));
      }
    };
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [cooldownUntil]);

  // Auto-dismiss the result note so it does not linger over the countdown.
  useEffect(() => {
    if (!note) return;
    const t = setTimeout(() => setNote(null), 5000);
    return () => clearTimeout(t);
  }, [note]);

  // Focus the confirm button on open; close on Escape (unless mid-request).
  useEffect(() => {
    if (!confirmOpen) return;
    confirmRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busy) setConfirmOpen(false);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [confirmOpen, busy]);

  const disabled = busy || remaining > 0;

  const doRestart = async () => {
    setBusy(true);
    setNote(null);
    try {
      await api('/api/services/portainer-agent/restart', { method: 'POST' });
      setNote({ kind: 'ok', text: 'Portainer agent redeployed' });
      const until = Date.now() + RESTART_COOLDOWN_MS;
      setCooldownUntil(until);
      window.localStorage.setItem(RESTART_COOLDOWN_KEY, String(until));
    } catch (e) {
      const body = (e as ApiError)?.body;
      const text =
        body && typeof body === 'object' && 'error' in body
          ? String((body as { error: unknown }).error)
          : 'Restart failed';
      setNote({ kind: 'err', text });
    } finally {
      setBusy(false);
      setConfirmOpen(false);
    }
  };

  return (
    <div className="mt-3">
      {(note || remaining > 0) && (
        <div
          className={clsx(
            'mb-1 rounded-md px-2.5 py-1 text-[11px] leading-snug',
            note?.kind === 'ok'
              ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/60 dark:text-emerald-300'
              : note?.kind === 'err'
                ? 'bg-red-50 text-red-700 dark:bg-red-950/60 dark:text-red-300'
                : 'bg-zinc-100 text-zinc-500 dark:bg-zinc-800 dark:text-zinc-400'
          )}
        >
          {note ? note.text : `Wieder nutzbar in ${remaining}s`}
        </div>
      )}

      <button
        type="button"
        onClick={() => { if (!disabled) setConfirmOpen(true); }}
        disabled={disabled}
        title="docker service update --force portainer_agent"
        className="inline-flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-sm text-zinc-600 transition hover:bg-zinc-100 hover:text-zinc-900 disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:bg-transparent dark:text-zinc-400 dark:hover:bg-zinc-900 dark:hover:text-zinc-100"
      >
        <RefreshCw className={clsx('h-4 w-4', busy && 'animate-spin')} />
        {busy ? 'Restarting…' : 'Restart Portainer'}
      </button>

      {confirmOpen && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
          onClick={() => { if (!busy) setConfirmOpen(false); }}
          role="dialog"
          aria-modal="true"
          aria-labelledby="restart-portainer-title"
        >
          <div className="card w-full max-w-sm" onClick={(e) => e.stopPropagation()}>
            <h3 id="restart-portainer-title" className="mb-2 text-base font-semibold">
              Restart Portainer agent?
            </h3>
            <p className="mb-4 text-sm text-zinc-600 dark:text-zinc-400">
              Runs{' '}
              <code className="rounded bg-zinc-100 px-1 py-0.5 text-[12px] dark:bg-zinc-800">
                docker service update --force portainer_agent
              </code>{' '}
              on the manager and redeploys every agent task with the same image. Expect a brief disruption while tasks restart.
            </p>
            <div className="flex justify-end gap-2">
              <button type="button" className="btn-ghost" onClick={() => setConfirmOpen(false)} disabled={busy}>
                Cancel
              </button>
              <button ref={confirmRef} type="button" className="btn-primary" onClick={doRestart} disabled={busy}>
                {busy && <RefreshCw className="h-4 w-4 animate-spin" />}
                {busy ? 'Restarting…' : 'Restart now'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
