'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { api, setCSRFToken } from '@/lib/api';
import type { UserInfo } from '@/lib/types';
import { Sidebar } from './sidebar';

interface Props {
  children: React.ReactNode;
}

/**
 * AuthShell — wraps every authenticated page.
 *
 * Resolution:
 *   • GET /api/me succeeds → mount shell + page
 *   • 401 / 403            → redirect to /login (real auth failure)
 *   • any other error      → keep retrying every 2s, show "Reconnecting…"
 *                            banner. Does NOT redirect — protects against
 *                            brief leader restarts kicking the user out.
 */
export function AuthShell({ children }: Props) {
  const [user, setUser] = useState<UserInfo | null>(null);
  const [reconnecting, setReconnecting] = useState(false);
  const router = useRouter();

  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const loadOnce = async () => {
      try {
        const me = await api<UserInfo>('/api/me');
        if (!alive) return;
        setCSRFToken(me.csrf_token);
        setUser(me);
        setReconnecting(false);
      } catch (e: unknown) {
        if (!alive) return;
        const err = e as { status?: number };
        if (err?.status === 401 || err?.status === 403) {
          router.replace('/login');
          return;
        }
        // Network / 5xx — keep the existing user state (if any) and retry.
        setReconnecting(true);
        timer = setTimeout(loadOnce, 2000);
      }
    };
    loadOnce();

    return () => {
      alive = false;
      if (timer) clearTimeout(timer);
    };
  }, [router]);

  if (!user) {
    return (
      <div className="flex h-screen items-center justify-center text-sm text-zinc-500">
        {reconnecting ? 'Reconnecting…' : 'Loading…'}
      </div>
    );
  }

  return (
    <div className="flex h-screen">
      <Sidebar userEmail={user.email} />
      <main className="flex-1 overflow-auto">
        {reconnecting && (
          <div className="border-b border-amber-200 bg-amber-50 px-4 py-1.5 text-center text-[11px] text-amber-900 dark:border-amber-900/40 dark:bg-amber-900/20 dark:text-amber-200">
            Reconnecting to leader…
          </div>
        )}
        {children}
      </main>
    </div>
  );
}
