'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { api } from '@/lib/api';

/**
 * Boot router. Resolves:
 *   • setup not completed     → /setup
 *   • setup done, logged in   → /nodes
 *   • setup done, not logged in (401/403) → /login
 *   • network / 5xx error     → retry every 2s, don't redirect (avoids
 *                               kicking the user out during brief restarts)
 */
export default function HomePage() {
  const router = useRouter();
  const [msg, setMsg] = useState('Loading…');

  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const tick = async () => {
      try {
        const status = await api<{ setup_completed: boolean }>('/api/setup/status');
        if (!alive) return;
        if (!status.setup_completed) {
          router.replace('/setup');
          return;
        }
      } catch (e: unknown) {
        if (!alive) return;
        const err = e as { status?: number };
        // Only treat a *successful* "not completed" as setup needed.
        // Network errors → retry. 4xx other than below → retry too.
        if (err?.status && err.status >= 400 && err.status < 500 && err.status !== 401 && err.status !== 403) {
          // Some other 4xx — probably a real client problem; show + retry anyway.
        }
        setMsg('Reconnecting…');
        timer = setTimeout(tick, 2000);
        return;
      }
      try {
        await api('/api/me');
        if (alive) router.replace('/nodes');
      } catch (e: unknown) {
        if (!alive) return;
        const err = e as { status?: number };
        if (err?.status === 401 || err?.status === 403) {
          router.replace('/login');
          return;
        }
        setMsg('Reconnecting…');
        timer = setTimeout(tick, 2000);
      }
    };
    tick();

    return () => {
      alive = false;
      if (timer) clearTimeout(timer);
    };
  }, [router]);

  return (
    <main className="flex h-screen items-center justify-center text-sm text-zinc-500">
      {msg}
    </main>
  );
}
