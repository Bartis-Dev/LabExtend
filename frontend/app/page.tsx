'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { api } from '@/lib/api';

export default function HomePage() {
  const router = useRouter();
  useEffect(() => {
    (async () => {
      try {
        const status = await api<{ setup_completed: boolean }>('/api/setup/status');
        if (!status.setup_completed) {
          router.replace('/setup');
          return;
        }
      } catch {
        // If status call fails, assume need setup.
        router.replace('/setup');
        return;
      }
      try {
        await api('/api/me');
        router.replace('/dashboard');
      } catch {
        router.replace('/login');
      }
    })();
  }, [router]);

  return (
    <main className="flex h-screen items-center justify-center text-sm text-zinc-500">
      Loading…
    </main>
  );
}
