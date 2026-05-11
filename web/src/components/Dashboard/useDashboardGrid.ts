import { useEffect, useState } from 'react';
import type { Layout } from 'react-grid-layout';
import { useUpdateLayout, type LayoutPayload } from '@/api/queries';
import type { Category, Service } from '@/api/types';

// useDashboardGrid keeps a debounced bridge between react-grid-layout's
// imperative onLayoutChange callbacks and the bulk PUT /api/layout
// endpoint, so a drag-end produces one network round-trip instead of N.
export function useDashboardGrid(services: Service[] | undefined, categories: Category[] | undefined) {
  const update = useUpdateLayout();

  // Pending diff buffered between drag and flush.
  const [outerLayouts, setOuterLayouts] = useState<Layout[] | null>(null);
  const [innerLayouts, setInnerLayouts] = useState<Record<number, Layout[]>>({});

  const flushOuter = (l: Layout[]) => setOuterLayouts(l);
  const flushInner = (catID: number, l: Layout[]) =>
    setInnerLayouts((prev) => ({ ...prev, [catID]: l }));

  useEffect(() => {
    if (!outerLayouts && Object.keys(innerLayouts).length === 0) return;
    const timer = setTimeout(() => {
      const payload: LayoutPayload = { services: [], categories: [] };
      const svcByID = new Map(services?.map((s) => [s.id, s]) ?? []);

      if (outerLayouts) {
        for (const item of outerLayouts) {
          if (item.i.startsWith('s-')) {
            const id = Number(item.i.slice(2));
            payload.services.push({
              id,
              x: item.x,
              y: item.y,
              w: item.w,
              h: item.h,
              category_id: null, // outer position implies no category
            });
          } else if (item.i.startsWith('c-')) {
            const id = Number(item.i.slice(2));
            payload.categories.push({ id, x: item.x, y: item.y, w: item.w, h: item.h });
          }
        }
      }
      for (const [catID, l] of Object.entries(innerLayouts)) {
        for (const item of l) {
          if (!item.i.startsWith('s-')) continue;
          const id = Number(item.i.slice(2));
          const prev = svcByID.get(id);
          if (!prev) continue;
          payload.services.push({
            id,
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

  // Build outer layout for current data.
  const outerLayout: Layout[] = [
    ...(categories ?? []).map((c) => ({
      i: `c-${c.id}`,
      x: c.layout.x,
      y: c.layout.y,
      w: Math.max(2, c.layout.w),
      h: Math.max(2, c.layout.h),
      minW: 2,
      minH: 2,
      maxW: 12,
      maxH: 12,
      // Outer drag handled via title bar only so card-interior clicks/drags don't move the category.
      // react-grid-layout uses draggableHandle on the GridLayout, applies to all items.
    })),
    ...(services ?? [])
      .filter((s) => s.category_id == null)
      .map((s) => ({
        i: `s-${s.id}`,
        x: s.layout.x,
        y: s.layout.y,
        w: Math.max(1, s.layout.w),
        h: Math.max(1, s.layout.h),
        minW: 1,
        minH: 1,
      })),
  ];

  return { outerLayout, flushOuter, flushInner };
}
