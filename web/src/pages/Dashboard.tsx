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
import {
  DND_MIME,
  readPayload,
  useMoveService,
} from '@/components/Dashboard/crossGridDnd';
import { FolderIcon, PlusIcon } from '@/components/icons';

const DEFAULT_COLS = 6;
const ROW_HEIGHT = 110;
const MARGIN = 10;
const CATEGORY_TITLE_PX = 36;
const CATEGORY_INNER_PADDING = 8;
const CATEGORY_INNER_MARGIN = 8;

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

  // cellPx: width of one outer grid cell, derived from container width and
  // the active column count. Drives both the outer cards and the inner-grid
  // sizing math so 1 inner cell ≈ 1 outer cell visually.
  const cellPx = (width - (safeCols - 1) * MARGIN) / safeCols;

  const move = useMoveService();
  const [dropOutside, setDropOutside] = useState(false);

  const acceptsSvc = (e: React.DragEvent) =>
    Array.from(e.dataTransfer.types).includes(DND_MIME);

  const onOuterDragOver = (e: React.DragEvent) => {
    if (!acceptsSvc(e)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    if (!dropOutside) setDropOutside(true);
  };

  const onOuterDragLeave = (e: React.DragEvent) => {
    // Only clear when leaving the container itself, not when entering a child.
    if (e.currentTarget === e.target) setDropOutside(false);
  };

  const onOuterDrop = (e: React.DragEvent) => {
    setDropOutside(false);
    const p = readPayload(e);
    if (!p) return;
    e.preventDefault();
    if (p.fromCategoryId === null) return; // already loose
    // Place at bottom of outer grid.
    const loose = (services.data ?? []).filter((s) => s.category_id == null);
    const cats = categories.data ?? [];
    const maxY = Math.max(
      0,
      ...loose.map((s) => s.layout.y + s.layout.h),
      ...cats.map((c) => c.layout.y + c.layout.h),
    );
    const w = Math.min(Math.max(1, p.w), safeCols);
    move.mutate({
      id: p.id,
      categoryId: null,
      x: 0,
      y: maxY,
      w,
      h: Math.max(1, p.h),
    });
  };

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

      <div
        ref={containerRef}
        onDragOver={onOuterDragOver}
        onDragLeave={onOuterDragLeave}
        onDrop={onOuterDrop}
        className={`rounded transition-shadow ${
          dropOutside ? 'shadow-[inset_0_0_0_2px_var(--accent)]' : ''
        }`}
      >
        {((services.data?.length ?? 0) > 0 || (categories.data?.length ?? 0) > 0) && (
          <GridLayout
            className="layout"
            layout={outerLayout}
            cols={safeCols}
            rowHeight={ROW_HEIGHT}
            width={width}
            margin={[MARGIN, MARGIN]}
            containerPadding={[0, 0]}
            compactType={null}
            preventCollision={false}
            isResizable
            isDraggable
            draggableHandle=".rgl-outer-drag"
            draggableCancel="button, a, input, select, textarea, .no-drag"
            onLayoutChange={(l) => flushOuter(l)}
          >
            {(categories.data ?? []).map((c) => {
              // Outer box size for this category, in pixels.
              const outerW = cellPx * c.layout.w + MARGIN * (c.layout.w - 1);
              const outerH = ROW_HEIGHT * c.layout.h + MARGIN * (c.layout.h - 1);
              // Inner grid usable area (minus title bar + inner padding).
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
