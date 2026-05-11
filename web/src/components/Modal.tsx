import type { ReactNode } from 'react';
import { useEffect } from 'react';
import { createPortal } from 'react-dom';
import { XIcon } from './icons';

type Props = {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  size?: 'sm' | 'md' | 'lg';
};

const sizes: Record<NonNullable<Props['size']>, string> = {
  sm: 'max-w-md',
  md: 'max-w-2xl',
  lg: 'max-w-4xl',
};

// Modal renders into document.body via a portal. This is required because
// react-grid-layout applies `transform: translate(...)` to every grid item,
// which creates a new containing block — a child `position: fixed` element
// would otherwise be trapped inside the parent card. The portal hops the
// modal up to <body>, escaping every transform/filter ancestor and putting
// it on the topmost stacking context for free.
export function Modal({ open, onClose, title, children, size = 'sm' }: Props) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    // Lock body scroll while a modal is up.
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      window.removeEventListener('keydown', onKey);
      document.body.style.overflow = prev;
    };
  }, [open, onClose]);

  if (!open) return null;

  const node = (
    <div
      className="fixed inset-0 z-[1000] grid place-items-center bg-black/60 p-4"
      onClick={onClose}
      onMouseDown={(e) => e.stopPropagation()}
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      <div
        className={`w-full ${sizes[size]} max-h-[90vh] overflow-y-auto rounded-lg border border-border bg-bg-card p-6 shadow-2xl`}
        onClick={(e) => e.stopPropagation()}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button
            onClick={onClose}
            className="rounded p-1 text-fg-muted hover:bg-bg-elevated hover:text-fg"
            aria-label="Close"
          >
            <XIcon />
          </button>
        </div>
        {children}
      </div>
    </div>
  );

  return createPortal(node, document.body);
}

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = 'Delete',
  danger = true,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  message: string;
  confirmLabel?: string;
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <Modal open={open} onClose={onCancel} title={title} size="sm">
      <p className="mb-6 text-fg-muted">{message}</p>
      <div className="flex justify-end gap-2">
        <button
          onClick={onCancel}
          className="rounded border border-border px-4 py-2 hover:bg-bg-elevated"
        >
          Cancel
        </button>
        <button
          onClick={onConfirm}
          className={`rounded px-4 py-2 text-white ${
            danger ? 'bg-danger hover:opacity-90' : 'bg-accent hover:bg-accent-hover'
          }`}
        >
          {confirmLabel}
        </button>
      </div>
    </Modal>
  );
}
