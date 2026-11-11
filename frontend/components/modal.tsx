'use client';

import { useEffect, useId, useRef } from 'react';
import { X } from 'lucide-react';

export const FOCUSABLE =
  'input:not([type=hidden]):not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])';

// Open dialogs, innermost last. Only the top one answers Escape, so a confirm
// opened over an edit dialog does not close both.
const stack: symbol[] = [];

/** trapTab keeps Tab and Shift+Tab cycling inside `root`. */
export function trapTab(e: React.KeyboardEvent, root: HTMLElement) {
  if (e.key !== 'Tab') return;
  const focusable = Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE))
    .filter((el) => el.offsetParent !== null);
  if (focusable.length === 0) {
    e.preventDefault();
    return;
  }
  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  if (e.shiftKey && (document.activeElement === first || document.activeElement === root)) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first.focus();
  }
}

/**
 * Modal is the shared dialog shell: backdrop, focus trap, Escape to close,
 * focus back to the opener on close, scroll lock, aria-modal.
 *
 * Initial focus goes to an element marked data-autofocus, else the first field
 * in the body, else the panel itself (never the close button).
 */
export function Modal({
  title,
  subtitle,
  icon,
  onClose,
  children,
  footer,
  width = 'max-w-lg',
  dismissable = true,
}: {
  title: string;
  subtitle?: React.ReactNode;
  icon?: React.ReactNode;
  onClose: () => void;
  children: React.ReactNode;
  footer?: React.ReactNode;
  width?: string;
  /** false while a request runs: Escape, backdrop and X do nothing. */
  dismissable?: boolean;
}) {
  const panel = useRef<HTMLDivElement>(null);
  const body = useRef<HTMLDivElement>(null);
  const titleId = useId();
  // Tracks where the pointer went down, so releasing outside after starting
  // inside (a selection drag) does not count as a backdrop click.
  const downOnBackdrop = useRef(false);
  // Callers often pass an inline arrow; reading it through a ref keeps the
  // mount effect from re-running (and re-focusing) on every keystroke.
  const close = useRef(onClose);
  close.current = onClose;
  const canClose = useRef(dismissable);
  canClose.current = dismissable;

  useEffect(() => {
    const me = Symbol('modal');
    stack.push(me);
    const opener = document.activeElement as HTMLElement | null;

    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && stack[stack.length - 1] === me && canClose.current) {
        e.stopPropagation();
        close.current();
      }
    };
    document.addEventListener('keydown', onKey);
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const target =
      panel.current?.querySelector<HTMLElement>('[data-autofocus]') ??
      body.current?.querySelector<HTMLElement>(FOCUSABLE) ??
      panel.current;
    target?.focus();

    return () => {
      document.removeEventListener('keydown', onKey);
      const i = stack.indexOf(me);
      if (i >= 0) stack.splice(i, 1);
      if (stack.length === 0) document.body.style.overflow = prevOverflow;
      // The opener may be gone (e.g. a deleted row); then focus just stays put.
      if (opener && document.contains(opener)) opener.focus();
    };
  }, []);

  // Keep Tab inside the dialog: without this, tabbing walks the page behind it.
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (panel.current) trapTab(e, panel.current);
  };

  return (
    <div
      className="fade-in fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-zinc-950/40 p-4 backdrop-blur-[2px] sm:items-center sm:p-6"
      onMouseDown={(e) => { downOnBackdrop.current = e.target === e.currentTarget; }}
      onMouseUp={(e) => {
        if (downOnBackdrop.current && e.target === e.currentTarget && canClose.current) close.current();
      }}
    >
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        onKeyDown={onKeyDown}
        className={`card modal-panel my-auto w-full ${width} p-0 shadow-xl outline-none focus-visible:ring-0`}
      >
        <header className="flex items-start gap-3 border-b border-zinc-200 px-6 py-4 dark:border-zinc-800">
          {icon && <span className="mt-0.5 shrink-0">{icon}</span>}
          <div className="min-w-0 flex-1">
            <h2 id={titleId} className="text-base font-semibold leading-tight">{title}</h2>
            {subtitle && <div className="mt-1 text-xs text-zinc-500">{subtitle}</div>}
          </div>
          <button
            type="button"
            onClick={() => { if (canClose.current) close.current(); }}
            disabled={!dismissable}
            aria-label="Close"
            className="btn-icon -mr-2 -mt-1"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div ref={body} className="px-6 py-5">{children}</div>

        {footer && (
          <footer className="flex flex-wrap items-center justify-end gap-2 border-t border-zinc-200 px-6 py-4 dark:border-zinc-800">
            {footer}
          </footer>
        )}
      </div>
    </div>
  );
}
