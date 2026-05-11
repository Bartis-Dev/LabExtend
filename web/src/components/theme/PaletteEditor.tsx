import { useState } from 'react';
import { Sketch } from '@uiw/react-color';
import type { Palette } from '@/api/types';

// PALETTE_KEYS is the canonical order in which the Quick Edit tab renders
// CSS variables. New variables added here automatically appear in the UI.
export const PALETTE_KEYS: { key: string; label: string }[] = [
  { key: '--bg', label: 'Background' },
  { key: '--bg-card', label: 'Card background' },
  { key: '--bg-elevated', label: 'Elevated surface' },
  { key: '--fg', label: 'Foreground (text)' },
  { key: '--fg-muted', label: 'Muted text' },
  { key: '--accent', label: 'Accent' },
  { key: '--accent-hover', label: 'Accent hover' },
  { key: '--border', label: 'Border' },
  { key: '--border-strong', label: 'Border (strong)' },
  { key: '--danger', label: 'Danger' },
  { key: '--success', label: 'Success' },
  { key: '--warning', label: 'Warning' },
];

export function PaletteEditor({
  palette,
  onChange,
}: {
  palette: Palette;
  onChange: (p: Palette) => void;
}) {
  const [openKey, setOpenKey] = useState<string | null>(null);
  const setOne = (key: string, value: string) => onChange({ ...palette, [key]: value });

  return (
    <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
      {PALETTE_KEYS.map(({ key, label }) => {
        const value = palette[key] ?? '#000000';
        return (
          <div
            key={key}
            className="relative flex items-center gap-3 rounded border border-border bg-bg-elevated/40 p-2"
          >
            <button
              type="button"
              onClick={() => setOpenKey(openKey === key ? null : key)}
              className="h-8 w-8 shrink-0 rounded border border-border"
              style={{ background: value }}
              aria-label={`Pick color for ${label}`}
            />
            <div className="min-w-0 flex-1">
              <div className="text-xs text-fg-muted">{label}</div>
              <div className="font-mono text-xs">{key}</div>
            </div>
            <input
              value={value}
              onChange={(e) => setOne(key, e.target.value)}
              className="w-28 rounded border border-border bg-bg-elevated px-2 py-1 font-mono text-xs"
            />
            {openKey === key && (
              <div className="absolute left-2 top-12 z-20">
                <Sketch
                  color={value}
                  onChange={(c) => setOne(key, c.hex)}
                  disableAlpha
                />
                <button
                  type="button"
                  onClick={() => setOpenKey(null)}
                  className="mt-2 w-full rounded bg-accent px-3 py-1 text-xs text-white"
                >
                  Close
                </button>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

export function defaultPalette(): Palette {
  return {
    '--bg': '#0a0a0a',
    '--bg-card': '#141414',
    '--bg-elevated': '#1c1c1c',
    '--fg': '#e5e5e5',
    '--fg-muted': '#9ca3af',
    '--accent': '#6366f1',
    '--accent-hover': '#818cf8',
    '--border': '#262626',
    '--border-strong': '#3f3f46',
    '--danger': '#ef4444',
    '--success': '#22c55e',
    '--warning': '#eab308',
  };
}
