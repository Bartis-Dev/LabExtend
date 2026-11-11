'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { api } from '@/lib/api';
import { DelayedSpinner } from '@/components/feedback';

/**
 * Boot router. Resolves:
 *   • logged in               → /nodes
 *   • not logged in (401/403) → /login
 *   • network / 5xx error     → retry every 2s, don't redirect (avoids
 *                               kicking the user out during brief restarts)
 */
export default function HomePage() {
  const router = useRouter();
  const [msg, setMsg] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const tick = async () => {
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
      {msg ?? <DelayedSpinner active className="h-5 w-5" />}
    </main>
  );
}
