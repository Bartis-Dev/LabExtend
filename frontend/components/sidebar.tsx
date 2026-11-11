'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useEffect, useState } from 'react';
import clsx from 'clsx';
import {
  Server, Boxes, Webhook, FolderOpen, Clock,
  HardDrive, Database, LogOut, ScrollText, UserCircle, RefreshCw, ChartColumn, Settings,
} from 'lucide-react';
import { api, errText, setCSRFToken } from '@/lib/api';
import type { UserInfo } from '@/lib/types';
import { NotificationBell } from './notifications';
import { confirmDialog } from './confirm';
import { toast } from './toast';
import { Tooltip } from './tooltip';

const NAV_ITEM_IDLE =
  'flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm text-zinc-700 transition hover:bg-zinc-100 hover:text-zinc-900 active:bg-zinc-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:text-zinc-300 dark:hover:bg-zinc-900 dark:hover:text-zinc-100 dark:active:bg-zinc-800';

// admin: page and its API are admin-only on the backend, so non-admins never see the link.
const NAV = [
  { href: '/nodes',      label: 'Nodes',     icon: Server },
  { href: '/containers', label: 'Containers',icon: Boxes },
  { href: '/analytics',  label: 'Web Analytics', icon: ChartColumn },
  { href: '/webhooks',   label: 'Webhooks',  icon: Webhook,    admin: true },
  { href: '/files',      label: 'Files',     icon: FolderOpen, admin: true },
  { href: '/cronjobs',   label: 'Cron',      icon: Clock,      admin: true },
  { href: '/s3',         label: 'S3',        icon: HardDrive,  admin: true },
  { href: '/backups',    label: 'Backups',   icon: Database,   admin: true },
  { href: '/audit',      label: 'Audit',     icon: ScrollText, admin: true },
  { href: '/settings',   label: 'Settings',  icon: Settings,   admin: true },
  { href: '/account',    label: 'Account',   icon: UserCircle },
];

export function Sidebar({ user }: { user: UserInfo }) {
  const pathname = usePathname();
  const nav = NAV.filter((item) =>
    (!item.admin || user.is_admin) &&
    (item.href !== '/audit' || user.audit_enabled));
  return (
    <aside className="flex h-screen w-64 flex-col overflow-y-auto border-r border-zinc-200 bg-white px-3 py-5 dark:border-zinc-800 dark:bg-zinc-950">
      <div className="flex items-start justify-between gap-2 px-2 pb-4">
        <div className="min-w-0">
          <div className="text-base font-semibold tracking-tight">LabExtend</div>
          <div className="truncate text-2xs text-zinc-500">{user.email}</div>
        </div>
        <NotificationBell />
      </div>

      <nav className="flex-1 space-y-0.5" aria-label="Main">
        {nav.map((item) => {
          const Icon = item.icon;
          const active = pathname === item.href || pathname.startsWith(item.href + '/');
          return (
            <Link
              key={item.href}
              href={item.href}
              aria-current={active ? 'page' : undefined}
              className={clsx(
                NAV_ITEM_IDLE,
                active && 'bg-zinc-100 font-medium text-zinc-950 dark:bg-zinc-800 dark:text-white',
              )}
            >
              <Icon className={clsx('h-4 w-4', active ? 'text-accent' : 'text-zinc-500 dark:text-zinc-400')} />
              {item.label}
            </Link>
          );
        })}
      </nav>

      {user.is_admin && <RestartPortainerButton />}

      <button
        type="button"
        onClick={async () => {
          try {
            await api('/api/auth/logout', { method: 'POST' });
          } catch {/* ignore */}
          setCSRFToken(null);
          window.location.href = '/login';
        }}
        className={NAV_ITEM_IDLE + ' mt-1'}
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
  const [cooldownUntil, setCooldownUntil] = useState(0);
  const [remaining, setRemaining] = useState(0);

  // Restore an in-flight cooldown across reloads.
  useEffect(() => {
    try {
      const raw = window.localStorage.getItem(RESTART_COOLDOWN_KEY);
      const until = raw ? Number(raw) : 0;
      if (until > Date.now()) setCooldownUntil(until);
      else if (raw) window.localStorage.removeItem(RESTART_COOLDOWN_KEY);
    } catch { /* blocked storage: no cooldown carried over */ }
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
        try { window.localStorage.removeItem(RESTART_COOLDOWN_KEY); } catch { /* ignore */ }
      } else {
        setRemaining(Math.ceil(ms / 1000));
      }
    };
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [cooldownUntil]);

  const disabled = busy || remaining > 0;

  const doRestart = async () => {
    const ok = await confirmDialog({
      title: 'Restart Portainer agent?',
      confirmLabel: 'Restart now',
      body: (
        <>
          Runs{' '}
          <code className="rounded bg-zinc-100 px-1 py-0.5 text-xs dark:bg-zinc-800">
            docker service update --force portainer_agent
          </code>{' '}
          on the manager and redeploys every agent task with the same image. Expect a brief disruption while tasks restart.
        </>
      ),
    });
    if (!ok) return;
    setBusy(true);
    try {
      await api('/api/services/portainer-agent/restart', { method: 'POST' });
      toast.success('Portainer agent redeployed.');
      const until = Date.now() + RESTART_COOLDOWN_MS;
      setCooldownUntil(until);
      try { window.localStorage.setItem(RESTART_COOLDOWN_KEY, String(until)); } catch { /* ignore */ }
    } catch (e) {
      toast.error('Restart failed: ' + errText(e, 'no answer from the leader'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mt-3">
      {remaining > 0 && (
        <div className="mb-1 rounded-md bg-zinc-100 px-2.5 py-1 text-2xs leading-snug text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300">
          Available again in {remaining}s
        </div>
      )}

      <Tooltip className="flex" label={<span className="font-mono">docker service update --force portainer_agent</span>}>
        <button
          type="button"
          onClick={() => { if (!disabled) doRestart(); }}
          disabled={disabled}
          className={NAV_ITEM_IDLE + ' w-full disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:bg-transparent'}
        >
          <RefreshCw className={clsx('h-4 w-4', busy && 'motion-safe:animate-spin')} />
          {busy ? 'Restarting…' : 'Restart Portainer'}
        </button>
      </Tooltip>
    </div>
  );
}
