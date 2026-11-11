'use client';

import { useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

/**
 * Tooltip replaces title="" where the hint matters (icon buttons, status
 * dots). It shows on hover after a short delay and immediately on keyboard
 * focus, and renders into body with fixed positioning so a table card with
 * overflow-hidden cannot clip it.
 */
export function Tooltip({ label, children, className = 'inline-flex' }: {
  label: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  const ref = useRef<HTMLSpanElement>(null);
  const [pos, setPos] = useState<{ x: number; y: number; below: boolean } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const id = useId();

  const open = (delay: number) => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      const r = ref.current?.getBoundingClientRect();
      if (!r) return;
      // Flip below when there is no room above (first rows near the top bar).
      const below = r.top < 48;
      setPos({ x: r.left + r.width / 2, y: below ? r.bottom + 6 : r.top - 6, below });
    }, delay);
  };
  const close = () => {
    if (timer.current) clearTimeout(timer.current);
    setPos(null);
  };

  useEffect(() => {
    if (!pos) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') close(); };
    window.addEventListener('keydown', onKey);
    window.addEventListener('scroll', close, true);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('scroll', close, true);
    };
  }, [pos]);

  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);

  return (
    <span
      ref={ref}
      className={className}
      aria-describedby={pos ? id : undefined}
      onMouseEnter={() => open(350)}
      onMouseLeave={close}
      // Keyboard focus only; a mouse click also focuses and should not pop it.
      onFocus={(e) => { if ((e.target as HTMLElement).matches(':focus-visible')) open(0); }}
      onBlur={close}
    >
      {children}
      {pos && createPortal(
        <span
          id={id}
          role="tooltip"
          className="fade-in pointer-events-none fixed z-[70] max-w-xs rounded-md bg-zinc-900 px-2.5 py-1.5 text-xs leading-snug text-white shadow-lg dark:bg-zinc-100 dark:text-zinc-900"
          style={{
            left: pos.x,
            top: pos.y,
            transform: pos.below ? 'translateX(-50%)' : 'translate(-50%, -100%)',
          }}
        >
          {label}
        </span>,
        document.body,
      )}
    </span>
  );
}
