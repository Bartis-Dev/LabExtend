'use client';

// SchedulePicker — toggles between a friendly preset/visual builder and a
// raw cron expression. Two modes:
//   variant="seconds" : 6-field cron (sec min hr dom mon dow) — backup runner
//   variant="cron"    : 5-field cron (min hr dom mon dow) — host crontab
//
// The raw expression is the source of truth; presets just write into it.
// onChange fires only on user input, never on mount, so opening an editor
// never rewrites a saved schedule.

import { useMemo, useState } from 'react';

type Variant = 'seconds' | 'cron';

interface Props {
  value: string;
  onChange: (v: string) => void;
  variant?: Variant;
}

interface Preset {
  label: string;
  build: (h: number, m: number, dow: number, dom: number) => string;
  needs: { hour?: boolean; minute?: boolean; dow?: boolean; dom?: boolean };
}

const presets = (variant: Variant): Record<string, Preset> => {
  const base = (rest: string) => (variant === 'seconds' ? `0 ${rest}` : rest);
  return {
    daily:   { label: 'Every day',        build: (h, m)             => base(`${m} ${h} * * *`),           needs: { hour: true, minute: true } },
    weekly:  { label: 'Every week',       build: (h, m, dow)        => base(`${m} ${h} * * ${dow}`),       needs: { hour: true, minute: true, dow: true } },
    monthly: { label: 'Every month',      build: (h, m, _dow, dom)  => base(`${m} ${h} ${dom} * *`),       needs: { hour: true, minute: true, dom: true } },
    hourly:  { label: 'Every hour',       build: (_h, m)            => base(`${m} * * * *`),               needs: { minute: true } },
    every6h: { label: 'Every 6 hours',    build: ()                 => base(`0 */6 * * *`),                needs: {} },
    every15m:{ label: 'Every 15 minutes', build: ()                 => base(`*/15 * * * *`),               needs: {} },
  };
};

const dows = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

interface Detected { preset: string; h: number; m: number; dow: number; dom: number }

const CUSTOM: Detected = { preset: 'custom', h: 3, m: 0, dow: 0, dom: 1 };

// Parse a cron string to guess which preset + hour/min/dow/dom produced it.
// Anything a preset cannot reproduce exactly (including a non-zero seconds
// field) is "custom", so the simple builder never silently changes it.
function detect(value: string, variant: Variant): Detected {
  const parts = value.trim().split(/\s+/);
  let fields = parts;
  if (variant === 'seconds') {
    if (parts.length !== 6 || parts[0] !== '0') return CUSTOM;
    fields = parts.slice(1);
  }
  if (fields.length !== 5) return CUSTOM;
  const [min, hr, dom, mon, dow] = fields;
  const num = (s: string, lo: number, hi: number) => {
    if (!/^\d+$/.test(s)) return NaN;
    const n = parseInt(s, 10);
    return n >= lo && n <= hi ? n : NaN;
  };
  if (mon !== '*') return CUSTOM;

  if (hr === '*/6' && min === '0' && dom === '*' && dow === '*') return { ...CUSTOM, preset: 'every6h', h: 0 };
  if (hr === '*' && min === '*/15' && dom === '*' && dow === '*') return { ...CUSTOM, preset: 'every15m', h: 0 };

  const m = num(min, 0, 59);
  if (isNaN(m)) return CUSTOM;
  if (hr === '*' && dom === '*' && dow === '*') return { ...CUSTOM, preset: 'hourly', h: 0, m };

  const h = num(hr, 0, 23);
  if (isNaN(h)) return CUSTOM;
  if (dom === '*' && dow === '*') return { ...CUSTOM, preset: 'daily', h, m };
  const wd = num(dow, 0, 6);
  if (dom === '*' && !isNaN(wd)) return { ...CUSTOM, preset: 'weekly', h, m, dow: wd };
  const md = num(dom, 1, 31);
  if (!isNaN(md) && dow === '*') return { ...CUSTOM, preset: 'monthly', h, m, dom: md };
  return CUSTOM;
}

// withCurrent returns opts plus `cur` (sorted in) when it is missing, so a
// saved value off the usual grid still shows instead of a blank select.
function withCurrent(opts: number[], cur: number): number[] {
  return opts.includes(cur) ? opts : [...opts, cur].sort((a, b) => a - b);
}

type Fields = { preset: string; hour: number; minute: number; dow: number; dom: number };

export function SchedulePicker({ value, onChange, variant = 'cron' }: Props) {
  const P = useMemo(() => presets(variant), [variant]);
  const [initial] = useState(() => detect(value, variant));
  const [mode, setMode] = useState<'simple' | 'cron'>(initial.preset === 'custom' ? 'cron' : 'simple');
  const [f, setF] = useState<Fields>({
    preset: initial.preset === 'custom' ? 'daily' : initial.preset,
    hour: initial.h, minute: initial.m, dow: initial.dow, dom: initial.dom,
  });

  // emit applies a simple-mode change and writes the rebuilt expression.
  // Called from event handlers only.
  const emit = (next: Partial<Fields>) => {
    const s = { ...f, ...next };
    setF(s);
    const p = P[s.preset];
    if (p) onChange(p.build(s.hour, s.minute, s.dow, s.dom));
  };

  const toSimple = () => {
    if (mode === 'simple') return;
    setMode('simple');
    const d = detect(value, variant);
    if (d.preset === 'custom') {
      // The typed expression has no simple form; the user asked for the
      // builder, so write what it shows.
      emit({});
      return;
    }
    setF({ preset: d.preset, hour: d.h, minute: d.m, dow: d.dow, dom: d.dom });
  };

  const cur = P[f.preset];
  const placeholder = variant === 'seconds' ? '0 0 3 * * *' : '0 3 * * *';
  const minuteOpts = withCurrent(
    Array.from({ length: 60 }, (_, i) => i).filter((m) => (cur?.needs.hour ? m % 5 === 0 : true)),
    f.minute,
  );
  const domOpts = withCurrent(Array.from({ length: 28 }, (_, i) => i + 1), f.dom);

  return (
    <div className="space-y-2">
      <div className="seg" role="radiogroup" aria-label="Schedule mode">
        <button type="button" role="radio" aria-checked={mode === 'simple'} onClick={toSimple}
          className={mode === 'simple' ? 'seg-item-active' : 'seg-item'}>
          Simple
        </button>
        <button type="button" role="radio" aria-checked={mode === 'cron'} onClick={() => setMode('cron')}
          className={mode === 'cron' ? 'seg-item-active' : 'seg-item'}>
          Cron expression
        </button>
      </div>

      {mode === 'simple' && cur && (
        <div className="space-y-2 rounded-md border border-zinc-200 bg-zinc-50 p-3 dark:border-zinc-800 dark:bg-zinc-900/40">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <select className="input h-9 w-40" aria-label="Repeat" value={f.preset} onChange={(e) => emit({ preset: e.target.value })}>
              {Object.entries(P).map(([k, p]) => <option key={k} value={k}>{p.label}</option>)}
            </select>

            {cur.needs.dow && (
              <select className="input h-9 w-24" aria-label="Day of week" value={f.dow} onChange={(e) => emit({ dow: +e.target.value })}>
                {dows.map((d, i) => <option key={i} value={i}>{d}</option>)}
              </select>
            )}
            {cur.needs.dom && (
              <select className="input h-9 w-20" aria-label="Day of month" value={f.dom} onChange={(e) => emit({ dom: +e.target.value })}>
                {domOpts.map((d) => <option key={d} value={d}>{d}.</option>)}
              </select>
            )}
            {cur.needs.hour && (
              <>
                <span className="text-zinc-500">at</span>
                <select className="input h-9 w-20" aria-label="Hour" value={f.hour} onChange={(e) => emit({ hour: +e.target.value })}>
                  {Array.from({ length: 24 }, (_, i) => i).map((h) => <option key={h} value={h}>{String(h).padStart(2, '0')}</option>)}
                </select>
                <span className="text-zinc-500">:</span>
              </>
            )}
            {cur.needs.minute && (
              <select className="input h-9 w-20" aria-label="Minute" value={f.minute} onChange={(e) => emit({ minute: +e.target.value })}>
                {minuteOpts.map((m) => <option key={m} value={m}>{String(m).padStart(2, '0')}</option>)}
              </select>
            )}
          </div>
          {cur.needs.dom && f.dom > 28 && (
            <p className="text-2xs text-amber-700 dark:text-amber-400">Months without day {f.dom} are skipped.</p>
          )}
          <p className="font-mono text-2xs text-zinc-500">→ {value || placeholder}</p>
        </div>
      )}

      {mode === 'cron' && (
        <div>
          <input className="input font-mono" aria-label="Cron expression" placeholder={placeholder} value={value} onChange={(e) => onChange(e.target.value)} />
          <p className="mt-1 text-2xs text-zinc-500">
            {variant === 'seconds'
              ? 'Format: sec min hour day-of-month month day-of-week'
              : 'Format: min hour day-of-month month day-of-week'}
          </p>
        </div>
      )}
    </div>
  );
}
