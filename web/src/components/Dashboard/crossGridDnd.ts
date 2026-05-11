import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '@/api/client';
import { categoriesKey, servicesKey } from '@/api/queries';
import type { Service } from '@/api/types';

// Custom MIME the drag carries so we don't react to unrelated drops
// (file drops, text drops, etc.).
export const DND_MIME = 'application/x-labextend-service';

export type DragPayload = {
  id: string;
  w: number;
  h: number;
  fromCategoryId: number | null;
};

export function readPayload(e: React.DragEvent): DragPayload | null {
  const raw = e.dataTransfer.getData(DND_MIME);
  if (!raw) return null;
  try {
    const p = JSON.parse(raw) as DragPayload;
    if (!p.id) return null;
    return p;
  } catch {
    return null;
  }
}

export function writePayload(e: React.DragEvent, p: DragPayload) {
  e.dataTransfer.setData(DND_MIME, JSON.stringify(p));
  e.dataTransfer.effectAllowed = 'move';
}

// Picks (x, y) for a service moved into a target grid. Items have arbitrary
// (x, y, w, h); we pick y just past the lowest occupied row so the dropped
// card lands at the bottom rather than overlapping.
export function nextFreePosition(
  existing: { x: number; y: number; w: number; h: number }[],
  w: number,
  _h: number,
  maxCols: number,
): { x: number; y: number } {
  if (existing.length === 0) return { x: 0, y: 0 };
  const maxY = existing.reduce((m, i) => Math.max(m, i.y + i.h), 0);
  return { x: 0, y: maxY, w: Math.min(w, maxCols) } as { x: number; y: number };
}

// useMoveService is a thin mutation that updates a service's category_id
// and layout via the bulk /api/layout endpoint. Uses optimistic cache
// updates so the card jumps to the target grid immediately on drop.
export function useMoveService() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (args: {
      id: string;
      categoryId: number | null;
      x: number;
      y: number;
      w: number;
      h: number;
    }) => {
      await api.put('/api/layout', {
        services: [
          {
            id: args.id,
            x: args.x,
            y: args.y,
            w: args.w,
            h: args.h,
            category_id: args.categoryId,
          },
        ],
        categories: [],
      });
    },
    onMutate: async (args) => {
      await qc.cancelQueries({ queryKey: servicesKey });
      const previous = qc.getQueryData<Service[]>(servicesKey);
      qc.setQueryData<Service[]>(servicesKey, (old) =>
        old?.map((s) =>
          s.id === args.id
            ? {
                ...s,
                category_id: args.categoryId,
                layout: { x: args.x, y: args.y, w: args.w, h: args.h },
              }
            : s,
        ) ?? old,
      );
      return { previous };
    },
    onError: (_err, _args, ctx) => {
      if (ctx?.previous) qc.setQueryData(servicesKey, ctx.previous);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: servicesKey });
      qc.invalidateQueries({ queryKey: categoriesKey });
    },
  });
}
