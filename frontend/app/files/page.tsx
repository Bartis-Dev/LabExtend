'use client';

import { AuthShell } from '@/components/auth-shell';

export default function FilesPage() {
  return (
    <AuthShell>
      <div className="mx-auto max-w-5xl px-6 py-10">
        <h1 className="text-2xl font-semibold tracking-tight">Files</h1>
        <p className="mt-1 text-sm text-zinc-500">Per-node filesystem browser with chown + UID/GID labels.</p>
        <div className="mt-6 rounded-lg border border-dashed border-zinc-300 p-12 text-center text-sm text-zinc-500 dark:border-zinc-700">
          Coming in phase 9.
        </div>
      </div>
    </AuthShell>
  );
}
