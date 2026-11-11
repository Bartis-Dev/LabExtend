'use client';

import { useEffect, useState } from 'react';
import { CircleAlert, LoaderCircle, RefreshCw } from 'lucide-react';

/**
 * Loading, empty and error states shared by every data page.
 *
 * Loading follows one rule: nothing for the first 400ms (most requests are
 * done by then and a flash of skeleton looks like flicker), then the skeleton
 * or spinner.
 */

export function useDelayed(active: boolean, ms = 400): boolean {
  const [show, setShow] = useState(false);
  useEffect(() => {
    if (!active) { setShow(false); return; }
    const t = setTimeout(() => setShow(true), ms);
    return () => clearTimeout(t);
  }, [active, ms]);
  return show;
}

export function Spinner({ className = 'h-4 w-4', label = 'Loading' }: { className?: string; label?: string }) {
  return (
    <span role="status" className="inline-flex items-center text-zinc-500">
      <LoaderCircle className={`${className} motion-safe:animate-spin`} aria-hidden />
      <span className="sr-only">{label}</span>
    </span>
  );
}

/** Spinner that only appears after the delay. For small inline waits. */
export function DelayedSpinner({ active, className }: { active: boolean; className?: string }) {
  const show = useDelayed(active);
  return show ? <Spinner className={className} /> : null;
}

/** A table-shaped skeleton inside a card. Shown only after the delay. */
export function TableSkeleton({ rows = 6, cols = 5 }: { rows?: number; cols?: number }) {
  const show = useDelayed(true);
  if (!show) return null;
  return (
    <div className="card fade-in overflow-hidden p-0" aria-busy="true" aria-label="Loading">
      <div className="flex gap-6 border-b border-zinc-200 px-4 py-3 dark:border-zinc-800">
        {Array.from({ length: cols }).map((_, i) => <div key={i} className="skeleton h-2.5 flex-1" />)}
      </div>
      <div className="divide-y divide-zinc-100 dark:divide-zinc-800/80">
        {Array.from({ length: rows }).map((_, r) => (
          <div key={r} className="flex items-center gap-6 px-4 py-4">
            {Array.from({ length: cols }).map((_, c) => (
              <div key={c} className="skeleton h-3 flex-1" style={{ opacity: c === 0 ? 1 : 0.7 }} />
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

/** Block skeleton for card-based pages (detail views, charts). */
export function BlockSkeleton({ blocks = 3 }: { blocks?: number }) {
  const show = useDelayed(true);
  if (!show) return null;
  return (
    <div className="fade-in space-y-4" aria-busy="true" aria-label="Loading">
      <div className="skeleton h-7 w-64" />
      <div className="skeleton h-4 w-96 max-w-full" />
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {Array.from({ length: blocks }).map((_, i) => <div key={i} className="skeleton h-40 rounded-xl" />)}
      </div>
    </div>
  );
}

export function EmptyState({
  icon,
  title,
  children,
  action,
  className = '',
}: {
  icon?: React.ReactNode;
  title: string;
  children?: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={`flex flex-col items-center px-6 py-14 text-center ${className}`}>
      {icon && <div className="mb-3 text-zinc-400 dark:text-zinc-500">{icon}</div>}
      <div className="text-sm font-semibold">{title}</div>
      {children && <div className="mt-1 max-w-md text-sm text-zinc-500">{children}</div>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

/** Error banner with the server's reason and an optional retry. */
export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div role="alert" className="alert-error mb-4 flex items-start gap-3">
      <CircleAlert className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
      <div className="min-w-0 flex-1 break-words">{message}</div>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="btn-sm -my-1 inline-flex items-center gap-1 rounded-md px-2 font-medium text-red-800 transition hover:bg-red-100 active:bg-red-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 dark:text-red-200 dark:hover:bg-red-900/50"
        >
          <RefreshCw className="h-3.5 w-3.5" /> Retry
        </button>
      )}
    </div>
  );
}
