import { useEffect, useState } from 'react';
import { Modal } from './Modal';
import { useCreateCategory, useUpdateCategory } from '@/api/queries';
import type { Category, CategoryInput } from '@/api/types';
import { ApiError } from '@/api/client';

type Props = {
  open: boolean;
  onClose: () => void;
  initial?: Category;
  defaultLayout?: { x: number; y: number; w: number; h: number };
};

export function CategoryForm({ open, onClose, initial, defaultLayout }: Props) {
  const [name, setName] = useState('');
  const [borderColor, setBorderColor] = useState('#3b82f6');
  const [size, setSize] = useState<'3x2' | '2x2'>('3x2');
  const [error, setError] = useState<string | null>(null);
  const create = useCreateCategory();
  const update = useUpdateCategory();

  useEffect(() => {
    if (!open) return;
    setError(null);
    if (initial) {
      setName(initial.name);
      setBorderColor(initial.border_color);
      setSize(initial.layout.w === 2 ? '2x2' : '3x2');
    } else {
      setName('');
      setBorderColor('#3b82f6');
      setSize('3x2');
    }
  }, [open, initial]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    const sizeMap = { '3x2': { w: 3, h: 2 }, '2x2': { w: 2, h: 2 } } as const;
    const payload: CategoryInput = {
      name,
      border_color: borderColor,
      layout: initial
        ? { ...initial.layout, ...sizeMap[size] }
        : { x: defaultLayout?.x ?? 0, y: defaultLayout?.y ?? 0, ...sizeMap[size] },
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
      size="sm"
    >
      <form onSubmit={submit} className="space-y-4">
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            autoFocus
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
          />
        </label>

        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Border color</span>
          <div className="flex items-center gap-2">
            <input
              type="color"
              value={borderColor}
              onChange={(e) => setBorderColor(e.target.value)}
              className="h-9 w-12 cursor-pointer rounded border border-border bg-bg-elevated"
            />
            <input
              value={borderColor}
              onChange={(e) => setBorderColor(e.target.value)}
              className="flex-1 rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
            />
          </div>
        </label>

        {!initial && (
          <label className="block">
            <span className="mb-1 block text-xs text-fg-muted">Default size</span>
            <select
              value={size}
              onChange={(e) => setSize(e.target.value as '3x2' | '2x2')}
              className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
            >
              <option value="3x2">3 × 2</option>
              <option value="2x2">2 × 2</option>
            </select>
            <span className="mt-1 block text-xs text-fg-muted">
              You can resize the category at any time by dragging its bottom-right corner.
            </span>
          </label>
        )}

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
