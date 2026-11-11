'use client';

import { useSyncExternalStore } from 'react';
import { TriangleAlert } from 'lucide-react';
import { Modal } from './modal';

/**
 * confirmDialog replaces window.confirm(): same one-line call site, but it
 * renders the app's Modal and resolves to true/false.
 *
 *   if (!(await confirmDialog({ title: 'Delete plan?', danger: true }))) return;
 *
 * <ConfirmHost /> in the root layout renders whichever request is open.
 */

export interface ConfirmOptions {
  title: string;
  body?: React.ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Red confirm button and a warning icon, for anything destructive. */
  danger?: boolean;
}

interface Pending extends ConfirmOptions { resolve: (ok: boolean) => void }

let pending: Pending | null = null;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

export function confirmDialog(opts: ConfirmOptions): Promise<boolean> {
  // A second request while one is open cancels the first rather than stacking.
  pending?.resolve(false);
  return new Promise((resolve) => {
    pending = { ...opts, resolve };
    emit();
  });
}

function settle(ok: boolean) {
  const p = pending;
  pending = null;
  emit();
  p?.resolve(ok);
}

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => { listeners.delete(l); };
};

export function ConfirmHost() {
  const p = useSyncExternalStore(subscribe, () => pending, () => null);
  if (!p) return null;
  return (
    <Modal
      key={p.title}
      title={p.title}
      width="max-w-md"
      icon={p.danger ? (
        <span className="flex h-8 w-8 items-center justify-center rounded-full bg-red-50 text-red-600 dark:bg-red-950/50 dark:text-red-400">
          <TriangleAlert className="h-4 w-4" />
        </span>
      ) : undefined}
      onClose={() => settle(false)}
      footer={
        <>
          <button type="button" onClick={() => settle(false)} className="btn-ghost" data-autofocus={p.danger ? true : undefined}>
            {p.cancelLabel ?? 'Cancel'}
          </button>
          <button
            type="button"
            onClick={() => settle(true)}
            className={p.danger ? 'btn-danger' : 'btn-primary'}
            data-autofocus={p.danger ? undefined : true}
          >
            {p.confirmLabel ?? 'Confirm'}
          </button>
        </>
      }
    >
      {p.body
        ? <div className="text-sm leading-relaxed text-zinc-600 dark:text-zinc-400">{p.body}</div>
        : <span className="sr-only">Confirm or cancel.</span>}
    </Modal>
  );
}
