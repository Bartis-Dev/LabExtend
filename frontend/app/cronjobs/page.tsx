'use client';

import { AuthShell } from '@/components/auth-shell';

export default function CronJobsPage() {
  return (
    <AuthShell>
      <div className="mx-auto max-w-5xl px-6 py-10">
        <h1 className="text-2xl font-semibold tracking-tight">Cron jobs</h1>
        <p className="mt-1 text-sm text-zinc-500">Per-node cron entries written to /etc/cron.d/bpm.</p>
        <div className="mt-6 rounded-lg border border-dashed border-zinc-300 p-12 text-center text-sm text-zinc-500 dark:border-zinc-700">
          Coming in phase 10.
        </div>
      </div>
    </AuthShell>
  );
}
