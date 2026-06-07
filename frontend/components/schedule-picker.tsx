'use client';

// SchedulePicker — toggles between a friendly preset/visual builder and a
// raw cron expression. Two modes:
//   variant="seconds" : 6-field cron (sec min hr dom mon dow) — backup runner
//   variant="cron"    : 5-field cron (min hr dom mon dow) — host crontab
//
// The raw expression is the source of truth; presets just write into it.

import { useEffect, useMemo, useState } from 'react';

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
    daily:   { label: 'Jeden Tag',        build: (h, m)             => base(`${m} ${h} * * *`),           needs: { hour: true, minute: true } },
    weekly:  { label: 'Jede Woche',       build: (h, m, dow)        => base(`${m} ${h} * * ${dow}`),       needs: { hour: true, minute: true, dow: true } },
    monthly: { label: 'Jeden Monat',      build: (h, m, _dow, dom)  => base(`${m} ${h} ${dom} * *`),       needs: { hour: true, minute: true, dom: true } },
    hourly:  { label: 'Stündlich',        build: (_h, m)            => base(`${m} * * * *`),               needs: { minute: true } },
    every6h: { label: 'Alle 6 Stunden',   build: ()                 => base(`0 */6 * * *`),                needs: {} },
    every15m:{ label: 'Alle 15 Minuten',  build: ()                 => base(`*/15 * * * *`),               needs: {} },
  };
};

const dows = ['So', 'Mo', 'Di', 'Mi', 'Do', 'Fr', 'Sa'];

// Parse a cron string to guess which preset + hour/min/dow/dom produced it.
// Best-effort — falls back to "custom" if nothing matches.
function detect(value: string, variant: Variant): { preset: string; h: number; m: number; dow: number; dom: number } {
  const parts = value.trim().split(/\s+/);
  const fields = variant === 'seconds'
    ? (parts.length === 6 ? parts.slice(1) : parts) // strip seconds
    : parts;
  if (fields.length !== 5) return { preset: 'custom', h: 3, m: 0, dow: 0, dom: 1 };
  const [min, hr, dom, mon, dow] = fields;
  const num = (s: string) => /^\d+$/.test(s) ? parseInt(s, 10) : NaN;

  if (mon === '*') {
    // every6h, every15m
    if (hr === '*/6' && min === '0' && dom === '*' && dow === '*') return { preset: 'every6h', h: 0, m: 0, dow: 0, dom: 1 };
    if (hr === '*'   && min === '*/15' && dom === '*' && dow === '*') return { preset: 'every15m', h: 0, m: 0, dow: 0, dom: 1 };

    // hourly: min N, hr *
    if (hr === '*' && !isNaN(num(min)) && dom === '*' && dow === '*') {
      return { preset: 'hourly', h: 0, m: num(min), dow: 0, dom: 1 };
    }
    // daily / weekly / monthly need numeric hr+min
    const h = num(hr), m = num(min);
    if (!isNaN(h) && !isNaN(m)) {
      if (dom === '*' && dow === '*')                 return { preset: 'daily', h, m, dow: 0, dom: 1 };
      if (dom === '*' && !isNaN(num(dow)))            return { preset: 'weekly', h, m, dow: num(dow), dom: 1 };
      if (!isNaN(num(dom)) && dow === '*')            return { preset: 'monthly', h, m, dow: 0, dom: num(dom) };
    }
  }
  return { preset: 'custom', h: 3, m: 0, dow: 0, dom: 1 };
}

export function SchedulePicker({ value, onChange, variant = 'cron' }: Props) {
  const P = useMemo(() => presets(variant), [variant]);
  const initial = useMemo(() => detect(value, variant), []); // eslint-disable-line react-hooks/exhaustive-deps
  const [mode, setMode] = useState<'simple' | 'cron'>(initial.preset === 'custom' ? 'cron' : 'simple');
  const [preset, setPreset] = useState<string>(initial.preset === 'custom' ? 'daily' : initial.preset);
  const [hour, setHour] = useState(initial.h);
  const [minute, setMinute] = useState(initial.m);
  const [dow, setDow] = useState(initial.dow);
  const [dom, setDom] = useState(initial.dom);

  // Rebuild whenever simple-mode inputs change.
  useEffect(() => {
    if (mode !== 'simple') return;
    const p = P[preset];
    if (!p) return;
    onChange(p.build(hour, minute, dow, dom));
  }, [mode, preset, hour, minute, dow, dom]); // eslint-disable-line react-hooks/exhaustive-deps

  const cur = P[preset];
  const placeholder = variant === 'seconds' ? '0 0 3 * * *' : '0 3 * * *';

  return (
    <div className="space-y-2">
      <div className="inline-flex rounded-md border border-zinc-200 p-0.5 text-xs dark:border-zinc-700">
        <button type="button" onClick={() => setMode('simple')}
          className={`rounded px-2 py-1 ${mode === 'simple' ? 'bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900' : 'text-zinc-600 dark:text-zinc-400'}`}>
          Einfach
        </button>
        <button type="button" onClick={() => setMode('cron')}
          className={`rounded px-2 py-1 ${mode === 'cron' ? 'bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900' : 'text-zinc-600 dark:text-zinc-400'}`}>
          Cron-Ausdruck
        </button>
      </div>

      {mode === 'simple' && cur && (
        <div className="space-y-2 rounded-md bg-zinc-50 p-3 dark:bg-zinc-900/40">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <select className="input h-8 w-36" value={preset} onChange={(e) => setPreset(e.target.value)}>
              {Object.entries(P).map(([k, p]) => <option key={k} value={k}>{p.label}</option>)}
            </select>

            {cur.needs.dow && (
              <select className="input h-8 w-24" value={dow} onChange={(e) => setDow(+e.target.value)}>
                {dows.map((d, i) => <option key={i} value={i}>{d}</option>)}
              </select>
            )}
            {cur.needs.dom && (
              <select className="input h-8 w-20" value={dom} onChange={(e) => setDom(+e.target.value)}>
                {Array.from({ length: 28 }, (_, i) => i + 1).map((d) => <option key={d} value={d}>{d}.</option>)}
              </select>
            )}
            {cur.needs.hour && (
              <>
                <span className="text-zinc-500">um</span>
                <select className="input h-8 w-20" value={hour} onChange={(e) => setHour(+e.target.value)}>
                  {Array.from({ length: 24 }, (_, i) => i).map((h) => <option key={h} value={h}>{String(h).padStart(2, '0')}</option>)}
                </select>
                <span className="text-zinc-500">:</span>
              </>
            )}
            {cur.needs.minute && (
              <select className="input h-8 w-20" value={minute} onChange={(e) => setMinute(+e.target.value)}>
                {Array.from({ length: 60 }, (_, i) => i).filter((m) => cur.needs.hour ? m % 5 === 0 : true).map((m) => <option key={m} value={m}>{String(m).padStart(2, '0')}</option>)}
              </select>
            )}
          </div>
          <p className="font-mono text-[11px] text-zinc-500">→ {value || placeholder}</p>
        </div>
      )}

      {mode === 'cron' && (
        <div>
          <input className="input font-mono" placeholder={placeholder} value={value} onChange={(e) => onChange(e.target.value)} />
          <p className="mt-1 text-[11px] text-zinc-500">
            {variant === 'seconds'
              ? 'Format: sec min hour day-of-month month day-of-week'
              : 'Format: min hour day-of-month month day-of-week'}
          </p>
        </div>
      )}
    </div>
  );
}
