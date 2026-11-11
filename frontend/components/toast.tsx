'use client';

import { useEffect, useSyncExternalStore } from 'react';
import { Check, X } from 'lucide-react';

/**
 * The one toast system. Call toast.success() / toast.error() from anywhere;
 * <Toaster /> in the root layout renders the stack. Toasts do not steal focus,
 * do not block the page and clear themselves.
 */

type Tone = 'success' | 'error';
interface Item { id: number; message: string; tone: Tone; timeoutMs: number }

let items: Item[] = [];
let nextId = 1;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

function push(message: string, tone: Tone, timeoutMs?: number) {
  // Errors stay longer: they usually carry text someone needs to read.
  const item = { id: nextId++, message, tone, timeoutMs: timeoutMs ?? (tone === 'error' ? 10_000 : 4000) };
  // Keep the stack short; an older toast is less relevant than a new one.
  items = [...items, item].slice(-4);
  emit();
}

function dismiss(id: number) {
  items = items.filter((t) => t.id !== id);
  emit();
}

export const toast = {
  success: (message: string, timeoutMs?: number) => push(message, 'success', timeoutMs),
  error: (message: string, timeoutMs?: number) => push(message, 'error', timeoutMs),
};

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => { listeners.delete(l); };
};
const snapshot = () => items;
const empty: Item[] = [];

export function Toaster() {
  const list = useSyncExternalStore(subscribe, snapshot, () => empty);
  return (
    <div
      aria-live="polite"
      className="pointer-events-none fixed inset-x-0 bottom-4 z-[60] flex flex-col items-center gap-2 px-4 sm:bottom-6"
    >
      {list.map((t) => <ToastItem key={t.id} item={t} />)}
    </div>
  );
}

function ToastItem({ item }: { item: Item }) {
  useEffect(() => {
    const t = setTimeout(() => dismiss(item.id), item.timeoutMs);
    return () => clearTimeout(t);
  }, [item.id, item.timeoutMs]);

  const isError = item.tone === 'error';
  return (
    <div
      role={isError ? 'alert' : 'status'}
      className="modal-panel pointer-events-auto flex w-full max-w-md items-start gap-3 rounded-lg border border-zinc-200 bg-white px-4 py-3 text-sm shadow-lg dark:border-zinc-700 dark:bg-zinc-900"
    >
      <span
        className={
          isError
            ? 'mt-px flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-red-100 text-red-700 dark:bg-red-900/50 dark:text-red-200'
            : 'mt-px flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-200'
        }
      >
        {isError ? <X className="h-3 w-3" /> : <Check className="h-3 w-3" />}
      </span>
      <span className="min-w-0 flex-1 whitespace-pre-line break-words">{item.message}</span>
      <button onClick={() => dismiss(item.id)} aria-label="Dismiss" className="btn-icon -my-1 -mr-2 h-7 w-7">
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}
