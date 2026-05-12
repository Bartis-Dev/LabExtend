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

const DEFAULT_COLS = 5;
const MARGIN = 12;
const CATEGORY_TITLE_PX = 36;
const CATEGORY_INNER_PADDING = 10;
const CATEGORY_INNER_MARGIN = 10;

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
    safeCols,
  );

  // cellPx: width of one outer grid cell. rowHeight is half of that so
  // a 1×1 cell is a wide rectangle (2:1) — readable at a glance for the
  // header + one host without wasting vertical space.
  const cellPx = (width - (safeCols - 1) * MARGIN) / safeCols;
  const rowHeight = Math.max(70, Math.round(cellPx / 2));

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
            rowHeight={rowHeight}
            width={width}
            margin={[MARGIN, MARGIN]}
            containerPadding={[0, 0]}
            compactType="vertical"
            preventCollision={false}
            isResizable
            isDraggable
            draggableHandle=".rgl-outer-drag"
            draggableCancel="button, a, input, select, textarea, .no-drag"
            onLayoutChange={(l) => flushOuter(l)}
          >
            {(categories.data ?? []).map((c) => {
              const outerW = cellPx * c.layout.w + MARGIN * (c.layout.w - 1);
              const outerH = rowHeight * c.layout.h + MARGIN * (c.layout.h - 1);
              const innerW = Math.max(80, outerW - CATEGORY_INNER_PADDING * 2);
              const innerCols = Math.max(1, c.layout.w);
              const usableH = outerH - CATEGORY_TITLE_PX - CATEGORY_INNER_PADDING * 2;
              const innerRows = Math.max(1, c.layout.h);
              const innerRowHeight = Math.max(
                50,
                (usableH - CATEGORY_INNER_MARGIN * (innerRows - 1)) / innerRows,
              );
              return (
                <div key={`c-${c.id}`}>
                  <CategoryCard
                    category={c}
                    services={servicesByCategory.get(c.id) ?? []}
                    innerWidth={innerW}
                    innerCols={innerCols}
                    innerRowHeight={innerRowHeight}
                    innerMargin={CATEGORY_INNER_MARGIN}
                    innerPadding={CATEGORY_INNER_PADDING}
                    onInnerLayoutChange={flushInner}
                  />
                </div>
              );
            })}
            {uncategorized.map((s) => (
              <div key={`s-${s.id}`} className="rgl-outer-drag">
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
