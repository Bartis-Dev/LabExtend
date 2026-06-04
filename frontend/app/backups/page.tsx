'use client';

import { AuthShell } from '@/components/auth-shell';

export default function BackupsPage() {
  return (
    <AuthShell>
      <div className="mx-auto max-w-5xl px-6 py-10">
        <h1 className="text-2xl font-semibold tracking-tight">Backups</h1>
        <p className="mt-1 text-sm text-zinc-500">Cron-scheduled, agent-streamed, S3-stored.</p>
        <div className="mt-6 rounded-lg border border-dashed border-zinc-300 p-12 text-center text-sm text-zinc-500 dark:border-zinc-700">
          Coming in phase 12.
        </div>
      </div>
    </AuthShell>
  );
}
