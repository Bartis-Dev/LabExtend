import { useEffect, useState } from 'react';
import Wheel from '@uiw/react-color-wheel';
import { hexToHsva, hsvaToHex } from '@uiw/color-convert';
import { Modal } from './Modal';
import {
  useCreateCategory,
  useServices,
  useSettings,
  useUpdateCategory,
} from '@/api/queries';
import type { Category, CategoryInput } from '@/api/types';
import { ApiError } from '@/api/client';

type Props = {
  open: boolean;
  onClose: () => void;
  initial?: Category;
  defaultLayout?: { x: number; y: number; w: number; h: number };
};

const DEFAULT_COLS = 6;
const MAX_HEIGHT = 10;
const DEFAULT_BORDER = '#475569';

// Curated muted swatches that read cleanly against a dark background.
// Tailwind 600-700 tier — saturated enough to identify, never neon.
const COLOR_PRESETS = [
  '#475569', // slate
  '#0f766e', // teal
  '#1d4ed8', // blue
  '#7c3aed', // violet
  '#a21caf', // fuchsia
  '#b91c1c', // red
  '#b45309', // amber
  '#15803d', // forest green
];

export function CategoryForm({ open, onClose, initial, defaultLayout }: Props) {
  const settings = useSettings();
  const services = useServices();
  const cols = clampInt(Number(settings.data?.grid_cols ?? DEFAULT_COLS), 4, 12);

  const [name, setName] = useState('');
  const [borderColor, setBorderColor] = useState(DEFAULT_BORDER);
  const [w, setW] = useState(3);
  const [h, setH] = useState(2);
  const [error, setError] = useState<string | null>(null);
  const create = useCreateCategory();
  const update = useUpdateCategory();

  // For an existing category, figure out the minimum (w, h) that still
  // fits all services currently inside. Used to block a manual shrink
  // that would lose cards.
  const inCat = initial
    ? (services.data ?? []).filter((s) => s.category_id === initial.id)
    : [];
  const minRequiredW = inCat.reduce(
    (m, s) => Math.max(m, s.layout.x + s.layout.w),
    1,
  );
  const minRequiredH = inCat.reduce(
    (m, s) => Math.max(m, s.layout.y + s.layout.h),
    1,
  );

  useEffect(() => {
    if (!open) return;
    setError(null);
    if (initial) {
      setName(initial.name);
      setBorderColor(initial.border_color);
      setW(initial.layout.w);
      setH(initial.layout.h);
    } else {
      setName('');
      setBorderColor('#3b82f6');
      setW(3);
      setH(2);
    }
  }, [open, initial]);

  // Clamp w/h to current grid bounds whenever cols changes.
  const safeW = clampInt(w, 1, cols);
  const safeH = clampInt(h, 1, MAX_HEIGHT);
  // The "1x2 or 2x1" minimum the spec calls for: enforce w*h >= 2.
  const validSize = safeW * safeH >= 2;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!validSize) {
      setError('Minimum size is 1×2 or 2×1.');
      return;
    }
    if (initial && (safeW < minRequiredW || safeH < minRequiredH)) {
      setError(
        `Too many services in this category. Minimum size for the current ${inCat.length} service(s): ${minRequiredW} × ${minRequiredH}. Remove or move some services first.`,
      );
      return;
    }
    const payload: CategoryInput = {
      name,
      border_color: borderColor,
      layout: initial
        ? { ...initial.layout, w: safeW, h: safeH }
        : { x: defaultLayout?.x ?? 0, y: defaultLayout?.y ?? 0, w: safeW, h: safeH },
    };
    try {
      if (initial) {
        await update.mutateAsync({ id: initial.id, input: payload });
      } else {
        await create.mutateAsync(payload);
      }
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'request failed');
    }
  };

  const busy = create.isPending || update.isPending;

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={initial ? `Edit ${initial.name}` : 'Add category'}
      size="md"
    >
      <form onSubmit={submit} className="space-y-5">
        <label className="block">
          <span className="mb-1 block text-xs uppercase tracking-wide text-fg-muted">
            Name
          </span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            autoFocus
            placeholder="e.g. Media, Network, Tools"
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
          />
        </label>

        <div className="grid grid-cols-2 gap-5">
          <div>
            <span className="mb-2 block text-xs uppercase tracking-wide text-fg-muted">
              Border color
            </span>
            <div className="space-y-3 rounded-lg border border-border bg-bg-elevated p-3">
              {/* Curated swatch palette — 80% of users should pick from here. */}
              <div className="grid grid-cols-8 gap-1.5">
                {COLOR_PRESETS.map((c) => {
                  const isActive = c.toLowerCase() === borderColor.toLowerCase();
                  return (
                    <button
                      key={c}
                      type="button"
                      onClick={() => setBorderColor(c)}
                      style={{ background: c }}
                      className={`h-7 w-full rounded transition-all ${
                        isActive
                          ? 'scale-110 ring-2 ring-fg/40'
                          : 'opacity-80 hover:scale-105 hover:opacity-100'
                      }`}
                      aria-label={`Color ${c}`}
                    />
                  );
                })}
              </div>

              <details className="text-xs">
                <summary className="cursor-pointer select-none text-fg-muted hover:text-fg">
                  Custom color
                </summary>
                <div className="mt-2 grid place-items-center">
                  <Wheel
                    width={140}
                    height={140}
                    color={hexToHsva(borderColor)}
                    onChange={(c) => setBorderColor(hsvaToHex(c.hsva))}
                  />
                </div>
                <div className="mt-2 flex items-center gap-2">
                  <div
                    className="h-7 w-7 shrink-0 rounded border border-border"
                    style={{ background: borderColor }}
                  />
                  <input
                    value={borderColor}
                    onChange={(e) => setBorderColor(e.target.value)}
                    className="flex-1 rounded border border-border bg-bg-card px-2 py-1 font-mono text-xs outline-none focus:border-accent"
                  />
                </div>
              </details>
            </div>
          </div>

          <div>
            <span className="mb-2 block text-xs uppercase tracking-wide text-fg-muted">
              Default size (grid units)
            </span>
            <div className="space-y-3 rounded-lg border border-border bg-bg-elevated p-3">
              <NumberStepper
                label="Width"
                value={safeW}
                onChange={setW}
                min={1}
                max={cols}
                hint={`max ${cols} (grid width)`}
              />
              <NumberStepper
                label="Height"
                value={safeH}
                onChange={setH}
                min={1}
                max={MAX_HEIGHT}
                hint={`max ${MAX_HEIGHT}`}
              />
              <div className="border-t border-border pt-2 text-xs text-fg-muted">
                Minimum total area: 1×2 or 2×1. Resize anytime by dragging the
                bottom-right corner of the category.
              </div>
            </div>
          </div>
        </div>

        <div
          className="rounded border-2 px-4 py-3 text-sm"
          style={{ borderColor }}
        >
          Preview — frame uses your border color.
        </div>

        {error && (
          <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
            {error}
          </div>
        )}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-border px-4 py-2 hover:bg-bg-elevated"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={busy}
            className="rounded bg-accent px-4 py-2 text-white hover:bg-accent-hover disabled:opacity-50"
          >
            {busy ? '…' : initial ? 'Save' : 'Create'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function NumberStepper({
  label,
  value,
  onChange,
  min,
  max,
  hint,
}: {
  label: string;
  value: number;
  onChange: (n: number) => void;
  min: number;
  max: number;
  hint?: string;
}) {
  const set = (n: number) => onChange(clampInt(n, min, max));
  return (
    <div>
      <div className="mb-1 flex items-baseline justify-between">
        <span className="text-xs text-fg-muted">{label}</span>
        {hint && <span className="text-[10px] text-fg-muted/70">{hint}</span>}
      </div>
      <div className="flex items-stretch gap-1">
        <button
          type="button"
          onClick={() => set(value - 1)}
          className="w-9 rounded border border-border bg-bg-card hover:bg-bg-elevated disabled:opacity-30"
          disabled={value <= min}
          aria-label={`${label} minus`}
        >
          −
        </button>
        <input
          type="number"
          value={value}
          min={min}
          max={max}
          onChange={(e) => set(Number(e.target.value))}
          className="w-16 rounded border border-border bg-bg-card px-2 py-1 text-center font-mono outline-none focus:border-accent"
        />
        <button
          type="button"
          onClick={() => set(value + 1)}
          className="w-9 rounded border border-border bg-bg-card hover:bg-bg-elevated disabled:opacity-30"
          disabled={value >= max}
          aria-label={`${label} plus`}
        >
          +
        </button>
      </div>
    </div>
  );
}

function clampInt(n: number, min: number, max: number): number {
  if (!Number.isFinite(n)) return min;
  return Math.max(min, Math.min(max, Math.floor(n)));
}
