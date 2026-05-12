import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

export type ContextMenuItem = {
  label: string;
  icon?: React.ReactNode;
  onClick: () => void;
  danger?: boolean;
  separator?: never;
} | {
  separator: true;
  label?: never;
  icon?: never;
  onClick?: never;
  danger?: never;
};

type Props = {
  open: boolean;
  x: number;
  y: number;
  items: ContextMenuItem[];
  onClose: () => void;
};

// ContextMenu renders into a body-level portal at (x, y), clamped to the
// viewport. Closes on outside click, scroll, escape, and any subsequent
// contextmenu event so a right-click on a different target moves the menu
// rather than stacking two.
export function ContextMenu({ open, x, y, items, onClose }: Props) {
  const menuRef = useRef<HTMLDivElement | null>(null);
  const [pos, setPos] = useState({ left: x, top: y });

  useLayoutEffect(() => {
    if (!open || !menuRef.current) return;
    const rect = menuRef.current.getBoundingClientRect();
    const left = Math.min(x, window.innerWidth - rect.width - 8);
    const top = Math.min(y, window.innerHeight - rect.height - 8);
    setPos({ left: Math.max(8, left), top: Math.max(8, top) });
  }, [open, x, y]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    const onPointer = (e: MouseEvent) => {
      if (menuRef.current?.contains(e.target as Node)) return;
      onClose();
    };
    const onScroll = () => onClose();
    window.addEventListener('keydown', onKey);
    window.addEventListener('mousedown', onPointer);
    window.addEventListener('scroll', onScroll, true);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('mousedown', onPointer);
      window.removeEventListener('scroll', onScroll, true);
    };
  }, [open, onClose]);

  if (!open) return null;

  const node = (
    <div
      ref={menuRef}
      role="menu"
      className="fixed z-[1100] min-w-[180px] overflow-hidden rounded-md border border-border bg-bg-card py-1 shadow-2xl"
      style={{ left: pos.left, top: pos.top }}
      onContextMenu={(e) => e.preventDefault()}
    >
      {items.map((item, i) =>
        item.separator ? (
          <div key={`sep-${i}`} className="my-1 h-px bg-border" />
        ) : (
          <button
            key={`item-${i}`}
            type="button"
            role="menuitem"
            onClick={() => {
              item.onClick?.();
              onClose();
            }}
            className={`flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm transition-colors hover:bg-bg-elevated ${
              item.danger ? 'text-danger' : 'text-fg'
            }`}
          >
            {item.icon && <span className="text-fg-muted">{item.icon}</span>}
            {item.label}
          </button>
        ),
      )}
    </div>
  );

  return createPortal(node, document.body);
}

// Cross-component coordination: dispatching this event tells every other
// useContextMenu instance to close. Solves the "two menus open at once"
// bug when the user right-clicks one card and then another without
// dismissing the first.
const CLOSE_ALL_EVENT = 'labextend:close-context-menus';

// Convenience hook for components that have a right-click menu.
export function useContextMenu() {
  const [state, setState] = useState<{ open: boolean; x: number; y: number }>({
    open: false,
    x: 0,
    y: 0,
  });

  useEffect(() => {
    const onClose = () => setState((s) => (s.open ? { ...s, open: false } : s));
    window.addEventListener(CLOSE_ALL_EVENT, onClose);
    return () => window.removeEventListener(CLOSE_ALL_EVENT, onClose);
  }, []);

  return {
    open: state.open,
    x: state.x,
    y: state.y,
    onContextMenu: (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      // Close any other open menus, then open ours.
      window.dispatchEvent(new Event(CLOSE_ALL_EVENT));
      setState({ open: true, x: e.clientX, y: e.clientY });
    },
    close: () => setState((s) => ({ ...s, open: false })),
  };
}
