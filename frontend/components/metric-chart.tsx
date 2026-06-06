'use client';

import { useMemo } from 'react';

export interface Series {
  label: string;
  color: string;
  values: number[];
}

interface Props {
  /** Unix-second timestamps for the X axis (length must match each series.values). */
  timestamps: number[];
  series: Series[];
  /** Pixel height of the SVG. Width is responsive. */
  height?: number;
  /** Fixed y-axis maximum (e.g. 100 for percentages). Auto-scale if undefined. */
  yMax?: number;
  /** Format a y-value for the right-side label. */
  formatY?: (v: number) => string;
  /** Show area fill under each line (lighter shade). */
  fill?: boolean;
}

/**
 * MetricChart — tiny SVG line chart. Zero deps, ~80 lines.
 *
 *   • Auto-y-scale unless yMax is set
 *   • Auto-grid (3 horizontal lines)
 *   • Stroked lines + optional area fill
 *   • Legend below
 *   • Empty / single-point series render gracefully
 */
export function MetricChart({
  timestamps, series, height = 160, yMax, formatY, fill = true,
}: Props) {
  const data = useMemo(() => {
    if (timestamps.length === 0 || series.every((s) => s.values.length === 0)) {
      return null;
    }
    let max = yMax ?? 0;
    if (yMax === undefined) {
      for (const s of series) {
        for (const v of s.values) if (v > max) max = v;
      }
      if (max <= 0) max = 1;
      max = niceMax(max);
    }
    const min = timestamps[0];
    const span = Math.max(1, timestamps[timestamps.length - 1] - min);
    return { max, min, span };
  }, [timestamps, series, yMax]);

  if (!data) {
    return <div className="flex h-40 items-center justify-center text-xs text-zinc-500">No data in window.</div>;
  }

  // 1000-wide viewBox, height as-given. Right margin for y-axis labels.
  const W = 1000;
  const H = height;
  const PADL = 40, PADR = 20, PADT = 8, PADB = 22;
  const innerW = W - PADL - PADR;
  const innerH = H - PADT - PADB;

  const xOf = (ts: number) => PADL + ((ts - data.min) / data.span) * innerW;
  const yOf = (v: number) => PADT + (1 - Math.min(1, v / data.max)) * innerH;

  // Horizontal grid + labels at 0%, 50%, 100% of max.
  const gridFracs = [0, 0.5, 1];
  const labelFmt = formatY ?? ((v: number) => v.toFixed(0));

  // X-axis: first + middle + last timestamp.
  const xTicks = [data.min, data.min + data.span / 2, data.min + data.span];

  return (
    <div className="w-full">
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full" preserveAspectRatio="none" style={{ height }}>
        {/* horizontal grid lines */}
        {gridFracs.map((f, i) => {
          const y = PADT + (1 - f) * innerH;
          return (
            <g key={i}>
              <line x1={PADL} x2={W - PADR} y1={y} y2={y} stroke="currentColor" strokeOpacity={0.08} />
              <text x={PADL - 6} y={y + 3} textAnchor="end" fontSize={10} fill="currentColor" opacity={0.5}>
                {labelFmt(data.max * f)}
              </text>
            </g>
          );
        })}
        {/* x-axis ticks (time) */}
        {xTicks.map((t, i) => (
          <text
            key={i}
            x={xOf(t)}
            y={H - 6}
            textAnchor={i === 0 ? 'start' : i === 2 ? 'end' : 'middle'}
            fontSize={10}
            fill="currentColor"
            opacity={0.5}
          >
            {fmtTime(t, data.span)}
          </text>
        ))}
        {/* series */}
        {series.map((s, i) => {
          if (s.values.length === 0) return null;
          const pts = s.values.map((v, idx) => `${xOf(timestamps[idx])},${yOf(v)}`).join(' ');
          const areaD = fill
            ? `M ${xOf(timestamps[0])},${yOf(0)} L ` +
              s.values.map((v, idx) => `${xOf(timestamps[idx])},${yOf(v)}`).join(' L ') +
              ` L ${xOf(timestamps[timestamps.length - 1])},${yOf(0)} Z`
            : '';
          return (
            <g key={i}>
              {fill && <path d={areaD} fill={s.color} opacity={0.08} />}
              <polyline points={pts} fill="none" stroke={s.color} strokeWidth={1.5} strokeLinejoin="round" />
            </g>
          );
        })}
      </svg>
      <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-zinc-500">
        {series.map((s, i) => {
          const last = s.values[s.values.length - 1] ?? 0;
          return (
            <span key={i} className="inline-flex items-center gap-1">
              <span className="inline-block h-2 w-2 rounded-sm" style={{ background: s.color }} />
              {s.label}: <span className="font-mono text-zinc-700 dark:text-zinc-300">{labelFmt(last)}</span>
            </span>
          );
        })}
      </div>
    </div>
  );
}

// niceMax rounds up to 1-2-5 multiples so the y-axis caps cleanly.
function niceMax(n: number): number {
  if (n <= 0) return 1;
  const pow = Math.pow(10, Math.floor(Math.log10(n)));
  const m = n / pow;
  let nice: number;
  if (m <= 1) nice = 1;
  else if (m <= 2) nice = 2;
  else if (m <= 5) nice = 5;
  else nice = 10;
  return nice * pow;
}

// fmtTime picks a short label depending on the visible span.
function fmtTime(ts: number, spanSec: number): string {
  const d = new Date(ts * 1000);
  if (spanSec < 3600) {
    // ≤ 1h → HH:MM:SS
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  }
  if (spanSec < 86400) {
    // ≤ 24h → HH:MM
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }
  // > 24h → DD.MM HH:MM
  return d.toLocaleString([], { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
}
