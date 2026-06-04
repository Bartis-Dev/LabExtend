'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import clsx from 'clsx';
import {
  LayoutDashboard, Server, Boxes, Bell, Webhook, FolderOpen, Clock, HardDrive, Database, LogOut
} from 'lucide-react';
import { api, setCSRFToken } from '@/lib/api';

const NAV = [
  { href: '/dashboard',  label: 'Dashboard', icon: LayoutDashboard },
  { href: '/nodes',      label: 'Nodes',     icon: Server },
  { href: '/containers', label: 'Containers',icon: Boxes },
  { href: '/alerts',     label: 'Alerts',    icon: Bell },
  { href: '/webhooks',   label: 'Webhooks',  icon: Webhook },
  { href: '/cronjobs',   label: 'Cron',      icon: Clock,    soon: true },
  { href: '/s3',         label: 'S3',        icon: HardDrive, soon: true },
  { href: '/backups',    label: 'Backups',   icon: Database, soon: true },
  { href: '/files',      label: 'Files',     icon: FolderOpen, soon: true },
];

export function Sidebar({ userEmail }: { userEmail?: string }) {
  const pathname = usePathname();
  return (
    <aside className="flex h-screen w-60 flex-col border-r border-zinc-200 bg-white px-3 py-4 dark:border-zinc-800 dark:bg-zinc-950">
      <div className="px-2 pb-4">
        <div className="text-sm font-semibold tracking-tight">labextend</div>
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
              {item.soon && <span className="text-[10px] text-zinc-400">soon</span>}
            </Link>
          );
        })}
      </nav>

      <button
        type="button"
        onClick={async () => {
          try {
            await api('/api/auth/logout', { method: 'POST' });
          } catch {/* ignore */}
          setCSRFToken(null);
          window.location.href = '/login';
        }}
        className="mt-3 inline-flex items-center gap-2 rounded-md px-2.5 py-1.5 text-sm text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900 dark:hover:bg-zinc-900 dark:hover:text-zinc-100"
      >
        <LogOut className="h-4 w-4" /> Sign out
      </button>
    </aside>
  );
}
