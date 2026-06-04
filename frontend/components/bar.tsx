'use client';

import clsx from 'clsx';

interface BarProps {
  label?: string;
  value: number;          // 0–100
  detail?: string;        // right-aligned secondary text
  className?: string;
  tone?: 'auto' | 'cool' | 'warn' | 'critical';
}

/**
 * Bar — Beszel-style minimal horizontal bar.
 *   • 0..100 width
 *   • Color band auto-thresholds (>=80 amber, >=90 red) unless overridden
 *   • Label + right-side detail
 */
export function Bar({ label, value, detail, className, tone = 'auto' }: BarProps) {
  const clamped = Math.max(0, Math.min(100, value || 0));
  let color = 'bg-emerald-500';
  if (tone === 'cool') color = 'bg-blue-500';
  if (tone === 'warn') color = 'bg-amber-500';
  if (tone === 'critical') color = 'bg-red-500';
  if (tone === 'auto') {
    if (clamped >= 90) color = 'bg-red-500';
    else if (clamped >= 75) color = 'bg-amber-500';
    else color = 'bg-emerald-500';
  }
  return (
    <div className={clsx('w-full', className)}>
      {(label || detail) && (
        <div className="mb-1 flex items-center justify-between text-xs">
          {label ? <span className="text-zinc-500">{label}</span> : <span />}
          {detail && <span className="font-mono text-zinc-700 dark:text-zinc-300">{detail}</span>}
        </div>
      )}
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-200 dark:bg-zinc-800">
        <div
          className={clsx('h-full rounded-full transition-all duration-300', color)}
          style={{ width: `${clamped}%` }}
        />
      </div>
    </div>
  );
}
