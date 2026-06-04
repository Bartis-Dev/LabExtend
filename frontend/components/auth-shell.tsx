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
 * AuthShell — wraps every authenticated page. Fetches /api/me, sets CSRF,
 * redirects to /login on 401. Renders the sidebar + page body.
 */
export function AuthShell({ children }: Props) {
  const [user, setUser] = useState<UserInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const router = useRouter();

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const me = await api<UserInfo>('/api/me');
        if (!alive) return;
        setCSRFToken(me.csrf_token);
        setUser(me);
      } catch (e: unknown) {
        const err = e as { status?: number };
        if (err?.status === 401 || err?.status === 403) {
          router.replace('/login');
          return;
        }
        if (alive) {
          router.replace('/');
        }
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [router]);

  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center text-sm text-zinc-500">
        Loading…
      </div>
    );
  }
  if (!user) return null;

  return (
    <div className="flex h-screen">
      <Sidebar userEmail={user.email} />
      <main className="flex-1 overflow-auto">{children}</main>
    </div>
  );
}
