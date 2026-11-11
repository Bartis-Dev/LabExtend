'use client';

import { createContext, useContext, useEffect, useState } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { Menu } from 'lucide-react';
import { api, setCSRFToken } from '@/lib/api';
import type { UserInfo } from '@/lib/types';
import { Sidebar } from './sidebar';
import { DelayedSpinner } from './feedback';

interface Props {
  children: React.ReactNode;
}

const MeContext = createContext<UserInfo | null>(null);

// useMe returns the signed-in user; only valid inside AuthShell.
export function useMe(): UserInfo {
  const me = useContext(MeContext);
  if (!me) throw new Error('useMe outside AuthShell');
  return me;
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
 *
 * Below lg the sidebar becomes a drawer behind a top bar menu button.
 */
export function AuthShell({ children }: Props) {
  const [user, setUser] = useState<UserInfo | null>(null);
  const [reconnecting, setReconnecting] = useState(false);
  const [navOpen, setNavOpen] = useState(false);
  const router = useRouter();
  const pathname = usePathname();

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

  useEffect(() => { setNavOpen(false); }, [pathname]);

  useEffect(() => {
    if (!navOpen) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setNavOpen(false); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [navOpen]);

  if (!user) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-3 text-sm text-zinc-500">
        {reconnecting ? 'Reconnecting to the leader…' : <DelayedSpinner active className="h-5 w-5" />}
      </div>
    );
  }

  return (
    <MeContext.Provider value={user}>
    <div className="flex h-screen">
      <div className="hidden lg:flex">
        <Sidebar user={user} />
      </div>

      {navOpen && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <div className="fade-in absolute inset-0 bg-zinc-950/40" onClick={() => setNavOpen(false)} aria-hidden />
          <div className="modal-panel absolute inset-y-0 left-0 flex shadow-xl">
            <Sidebar user={user} />
          </div>
        </div>
      )}

      <main className="min-w-0 flex-1 overflow-auto">
        <div className="sticky top-0 z-30 flex h-14 items-center gap-3 border-b border-zinc-200 bg-white/90 px-4 backdrop-blur lg:hidden dark:border-zinc-800 dark:bg-zinc-950/90">
          <button
            type="button"
            onClick={() => setNavOpen(true)}
            aria-label="Open navigation"
            aria-expanded={navOpen}
            className="btn-icon h-9 w-9"
          >
            <Menu className="h-5 w-5" />
          </button>
          <span className="text-sm font-semibold tracking-tight">LabExtend</span>
        </div>
        {reconnecting && (
          <div role="status" className="border-b border-amber-200 bg-amber-50 px-4 py-2 text-center text-xs text-amber-900 dark:border-amber-900/40 dark:bg-amber-900/20 dark:text-amber-200">
            Reconnecting to leader…
          </div>
        )}
        {children}
      </main>
    </div>
    </MeContext.Provider>
  );
}
