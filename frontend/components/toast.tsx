'use client';

import { useEffect } from 'react';
import { Check, X } from 'lucide-react';

/**
 * Toast is the success feedback that used to be a window.alert(): it does not
 * steal focus, does not block the page, and clears itself.
 */
export function Toast({ message, onDone, tone = 'success', timeoutMs = 4000 }: {
  message: string;
  onDone: () => void;
  tone?: 'success' | 'error';
  timeoutMs?: number;
}) {
  useEffect(() => {
    const t = setTimeout(onDone, timeoutMs);
    return () => clearTimeout(t);
  }, [onDone, timeoutMs]);

  const isError = tone === 'error';

  return (
    <div
      role="status"
      aria-live="polite"
      className="modal-panel fixed bottom-5 left-1/2 z-50 flex -translate-x-1/2 items-center gap-2.5 rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 text-sm shadow-lg dark:border-zinc-700 dark:bg-zinc-900"
    >
      <span
        className={
          isError
            ? 'flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-red-100 text-red-600 dark:bg-red-900/50 dark:text-red-300'
            : 'flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-emerald-100 text-emerald-600 dark:bg-emerald-900/50 dark:text-emerald-300'
        }
      >
        {isError ? <X className="h-3 w-3" /> : <Check className="h-3 w-3" />}
      </span>
      <span>{message}</span>
      <button
        onClick={onDone}
        aria-label="Dismiss"
        className="-mr-1 rounded p-1 text-zinc-400 transition hover:bg-zinc-100 hover:text-zinc-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}
