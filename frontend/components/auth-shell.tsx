'use client';

import { createContext, useContext, useEffect, useRef, useState } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { Menu } from 'lucide-react';
import { api, setCSRFToken } from '@/lib/api';
import type { UserInfo } from '@/lib/types';
import { Sidebar } from './sidebar';
import { DelayedSpinner } from './feedback';
import { FOCUSABLE, trapTab } from './modal';

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
 *   • any other error      → keep retrying every 2s and show "Reconnecting…"
 *                            instead of the spinner. Does NOT redirect:
 *                            protects against brief leader restarts.
 * Once mounted, a later expired session is caught by api() itself (401 on
 * any call redirects to /login).
 *
 * Below lg the sidebar becomes a drawer behind a top bar menu button. Only
 * one Sidebar is mounted at a time, so there is a single NotificationBell
 * (one SSE connection, no duplicate toasts).
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

  // Crossing into lg shows the desktop sidebar again, so drop the drawer.
  useEffect(() => {
    const mq = window.matchMedia('(min-width: 1024px)');
    const onChange = () => { if (mq.matches) setNavOpen(false); };
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);

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
      {navOpen ? (
        <NavDrawer user={user} onClose={() => setNavOpen(false)} />
      ) : (
        <div className="hidden lg:flex">
          <Sidebar user={user} />
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
        {children}
      </main>
    </div>
    </MeContext.Provider>
  );
}

// NavDrawer is the mobile sidebar: a modal dialog with focus moved in, Tab
// trapped, Escape and backdrop to close, and focus back to the opener.
function NavDrawer({ user, onClose }: { user: UserInfo; onClose: () => void }) {
  const panel = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  close.current = onClose;

  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    panel.current?.querySelector<HTMLElement>(FOCUSABLE)?.focus();
    return () => { if (opener && document.contains(opener)) opener.focus(); };
  }, []);

  return (
    <div className="fixed inset-0 z-40 lg:hidden">
      <div className="fade-in absolute inset-0 bg-zinc-950/40" onClick={onClose} aria-hidden />
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-label="Navigation"
        onKeyDown={(e) => {
          if (e.key === 'Escape') { e.stopPropagation(); close.current(); return; }
          if (panel.current) trapTab(e, panel.current);
        }}
        className="modal-panel absolute inset-y-0 left-0 flex shadow-xl"
      >
        <Sidebar user={user} />
      </div>
    </div>
  );
}
