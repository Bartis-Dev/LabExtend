import { useEffect, useState } from 'react';
import type { Layout } from 'react-grid-layout';
import { useUpdateLayout, type LayoutPayload } from '@/api/queries';
import type { Category, Service } from '@/api/types';

// useDashboardGrid keeps a debounced bridge between react-grid-layout's
// imperative onLayoutChange callbacks and the bulk PUT /api/layout
// endpoint, so a drag-end produces one network round-trip instead of N.
//
// The same hook also derives the *current* layout from the data, clamping
// any persisted coordinates that no longer fit the active grid_cols (e.g.
// when the user reduces the column count from 6 to 4). Clamping happens
// purely client-side; the next drag flushes corrected values back to the
// server.
export function useDashboardGrid(
  services: Service[] | undefined,
  categories: Category[] | undefined,
  cols: number,
) {
  const update = useUpdateLayout();

  const [outerLayouts, setOuterLayouts] = useState<Layout[] | null>(null);
  const [innerLayouts, setInnerLayouts] = useState<Record<number, Layout[]>>({});

  const flushOuter = (l: Layout[]) => setOuterLayouts(l);
  const flushInner = (catID: number, l: Layout[]) =>
    setInnerLayouts((prev) => ({ ...prev, [catID]: l }));

  useEffect(() => {
    if (!outerLayouts && Object.keys(innerLayouts).length === 0) return;
    const timer = setTimeout(() => {
      const payload: LayoutPayload = { services: [], categories: [] };

      if (outerLayouts) {
        for (const item of outerLayouts) {
          if (item.i.startsWith('s-')) {
            payload.services.push({
              id: item.i.slice(2),
              x: item.x,
              y: item.y,
              w: item.w,
              h: item.h,
              category_id: null,
            });
          } else if (item.i.startsWith('c-')) {
            payload.categories.push({
              id: Number(item.i.slice(2)),
              x: item.x,
              y: item.y,
              w: item.w,
              h: item.h,
            });
          }
        }
      }
      for (const [catID, l] of Object.entries(innerLayouts)) {
        for (const item of l) {
          if (!item.i.startsWith('s-')) continue;
          payload.services.push({
            id: item.i.slice(2),
            x: item.x,
            y: item.y,
            w: item.w,
            h: item.h,
            category_id: Number(catID),
          });
        }
      }
      if (payload.services.length === 0 && payload.categories.length === 0) return;
      update.mutate(payload);
      setOuterLayouts(null);
      setInnerLayouts({});
    }, 300);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [outerLayouts, innerLayouts]);

  // Build outer layout, clamping everything to current cols.
  const safeCols = Math.max(2, cols);

  // For each category, compute the minimum (w, h) needed to fit its
  // current inner services. RGL's minW/minH bind the resize handle so
  // the user simply cannot drag a category smaller than the area its
  // services already occupy — no error dialog needed, the handle just
  // stops moving once the threshold is reached.
  const requiredByCat = new Map<number, { w: number; h: number }>();
  for (const s of services ?? []) {
    if (s.category_id == null) continue;
    const prev = requiredByCat.get(s.category_id) ?? { w: 1, h: 1 };
    requiredByCat.set(s.category_id, {
      w: Math.max(prev.w, s.layout.x + s.layout.w),
      h: Math.max(prev.h, s.layout.y + s.layout.h),
    });
  }

  const catItems: Layout[] = (categories ?? []).map((c) => {
    let w = clamp(c.layout.w, 1, safeCols);
    let h = clamp(c.layout.h, 1, 10);
    // Categories must have at least 2 cells of area (1×2, 2×1, …).
    if (w * h < 2) {
      if (w <= h) h = 2;
      else w = 2;
    }
    const req = requiredByCat.get(c.id) ?? { w: 1, h: 1 };
    const x = clamp(c.layout.x, 0, Math.max(0, safeCols - w));
    return {
      i: `c-${c.id}`,
      x,
      y: Math.max(0, c.layout.y),
      w,
      h,
      minW: Math.max(1, req.w),
      minH: Math.max(1, req.h),
      maxW: safeCols,
      maxH: 10,
    };
  });

  const looseSvcItems: Layout[] = (services ?? [])
    .filter((s) => s.category_id == null)
    .map((s) => {
      const w = clamp(s.layout.w, 1, safeCols);
      const h = clamp(s.layout.h, 1, 10);
      const x = clamp(s.layout.x, 0, Math.max(0, safeCols - w));
      return {
        i: `s-${s.id}`,
        x,
        y: Math.max(0, s.layout.y),
        w,
        h,
        minW: 1,
        minH: 1,
        maxW: safeCols,
        maxH: 10,
        // Service cards are not user-resizable — their size is implied
        // by the grid baseline (1×1 = one card). Only categories resize.
        isResizable: false,
      };
    });

  const outerLayout: Layout[] = [...catItems, ...looseSvcItems];

  return { outerLayout, flushOuter, flushInner };
}

function clamp(n: number, min: number, max: number): number {
  if (!Number.isFinite(n)) return min;
  return Math.max(min, Math.min(max, Math.floor(n)));
}
