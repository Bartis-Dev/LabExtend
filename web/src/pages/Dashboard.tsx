import { useEffect, useRef, useState } from 'react';
import GridLayout from 'react-grid-layout';
import {
  useCategories,
  useServices,
  useSettings,
} from '@/api/queries';
import { ServiceCard } from '@/components/ServiceCard';
import { ServiceForm } from '@/components/ServiceForm';
import { CategoryCard } from '@/components/CategoryCard';
import { CategoryForm } from '@/components/CategoryForm';
import { useDashboardGrid } from '@/components/Dashboard/useDashboardGrid';
import { FolderIcon, PlusIcon } from '@/components/icons';

const DEFAULT_COLS = 6;
const ROW_HEIGHT = 110;

export default function Dashboard() {
  const [addServiceOpen, setAddServiceOpen] = useState(false);
  const [addCategoryOpen, setAddCategoryOpen] = useState(false);
  const services = useServices();
  const categories = useCategories();
  const settings = useSettings();

  const cols = Number(settings.data?.grid_cols ?? DEFAULT_COLS);
  const safeCols = Number.isFinite(cols) && cols >= 4 && cols <= 12 ? cols : DEFAULT_COLS;

  const containerRef = useRef<HTMLDivElement | null>(null);
  const [width, setWidth] = useState(1200);

  useEffect(() => {
    if (!containerRef.current) return;
    const ro = new ResizeObserver(([entry]) => {
      const w = entry.contentRect.width;
      if (w > 0) setWidth(w);
    });
    ro.observe(containerRef.current);
    return () => ro.disconnect();
  }, []);

  const { outerLayout, flushOuter, flushInner } = useDashboardGrid(
    services.data,
    categories.data,
  );

  const cellPx = (width - (safeCols + 1) * 10) / safeCols;

  const uncategorized = (services.data ?? []).filter((s) => s.category_id == null);
  const servicesByCategory = new Map<number, typeof uncategorized>();
  for (const s of services.data ?? []) {
    if (s.category_id != null) {
      const arr = servicesByCategory.get(s.category_id) ?? [];
      arr.push(s);
      servicesByCategory.set(s.category_id, arr);
    }
  }

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setAddCategoryOpen(true)}
            className="flex items-center gap-2 rounded border border-border px-3 py-2 hover:bg-bg-elevated"
          >
            <FolderIcon /> Add category
          </button>
          <button
            onClick={() => setAddServiceOpen(true)}
            className="flex items-center gap-2 rounded bg-accent px-4 py-2 font-semibold text-white hover:bg-accent-hover"
          >
            <PlusIcon /> Add service
          </button>
        </div>
      </div>

      {(services.isLoading || categories.isLoading) && (
        <div className="text-fg-muted">Loading…</div>
      )}

      {services.data?.length === 0 && categories.data?.length === 0 && (
        <div className="rounded border border-dashed border-border p-12 text-center text-fg-muted">
          No services yet. Click <span className="text-fg">Add service</span> to get started.
        </div>
      )}

      <div ref={containerRef}>
        {((services.data?.length ?? 0) > 0 || (categories.data?.length ?? 0) > 0) && (
          <GridLayout
            className="layout"
            layout={outerLayout}
            cols={safeCols}
            rowHeight={ROW_HEIGHT}
            width={width}
            margin={[10, 10]}
            containerPadding={[0, 0]}
            compactType={null}
            preventCollision={false}
            isResizable
            isDraggable
            draggableCancel="button, a, input, select, textarea"
            onLayoutChange={(l) => flushOuter(l)}
          >
            {(categories.data ?? []).map((c) => {
              // Inner width comes from outer cell math: c.layout.w cells wide minus inner padding.
              const innerW = Math.max(80, cellPx * c.layout.w + 10 * (c.layout.w - 1) - 8);
              const innerCols = Math.max(1, c.layout.w);
              return (
                <div key={`c-${c.id}`}>
                  <CategoryCard
                    category={c}
                    services={servicesByCategory.get(c.id) ?? []}
                    cellPx={cellPx * 0.55}
                    innerWidth={innerW}
                    innerCols={innerCols}
                    onInnerLayoutChange={flushInner}
                  />
                </div>
              );
            })}
            {uncategorized.map((s) => (
              <div key={`s-${s.id}`}>
                <ServiceCard service={s} />
              </div>
            ))}
          </GridLayout>
        )}
      </div>

      <ServiceForm open={addServiceOpen} onClose={() => setAddServiceOpen(false)} />
      <CategoryForm open={addCategoryOpen} onClose={() => setAddCategoryOpen(false)} />
    </div>
  );
}
