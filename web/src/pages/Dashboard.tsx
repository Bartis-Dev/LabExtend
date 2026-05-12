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
import { ContextMenu, useContextMenu } from '@/components/ContextMenu';
import { FolderIcon, GripIcon, PlusIcon } from '@/components/icons';

const DEFAULT_COLS = 5;
const MARGIN = 24;
const CATEGORY_TITLE_PX = 40;
const CATEGORY_INNER_PADDING = 20;
const CATEGORY_INNER_MARGIN = 20;

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

  // Background right-click → Add service / Add category. Cards/categories
  // stopPropagation on their own contextmenu so this only fires when the
  // user clicked truly empty space.
  const bgMenu = useContextMenu();

  const handleBgContextMenu = (e: React.MouseEvent) => {
    // Only fire if the right-click is on the dashboard background itself,
    // not bubbling from a child card/category that didn't intercept.
    e.preventDefault();
    bgMenu.onContextMenu(e);
  };

  return (
    <div className="p-6">
      <div className="mb-5 flex items-center justify-between">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <p className="text-xs text-fg-muted">
          Right-click anywhere for actions · drag the
          <GripIcon className="-mt-0.5 mx-1 inline-block align-middle" width={12} height={12} />
          handle to reorder
        </p>
      </div>

      {(services.isLoading || categories.isLoading) && (
        <div className="text-fg-muted">Loading…</div>
      )}

      {services.data?.length === 0 && categories.data?.length === 0 && (
        <div
          className="rounded border border-dashed border-border p-12 text-center text-fg-muted"
          onContextMenu={handleBgContextMenu}
        >
          No services yet. <span className="text-fg">Right-click anywhere</span> to add a
          service or a category.
        </div>
      )}

      <div
        ref={containerRef}
        onContextMenu={handleBgContextMenu}
        className="min-h-[200px]"
      >
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
            draggableHandle=".rgl-drag-handle"
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
              <div key={`s-${s.id}`}>
                <ServiceCard service={s} />
              </div>
            ))}
          </GridLayout>
        )}
      </div>

      <ServiceForm open={addServiceOpen} onClose={() => setAddServiceOpen(false)} />
      <CategoryForm open={addCategoryOpen} onClose={() => setAddCategoryOpen(false)} />

      <ContextMenu
        open={bgMenu.open}
        x={bgMenu.x}
        y={bgMenu.y}
        onClose={bgMenu.close}
        items={[
          {
            label: 'Add service',
            icon: <PlusIcon width={14} height={14} />,
            onClick: () => setAddServiceOpen(true),
          },
          {
            label: 'Add category',
            icon: <FolderIcon width={14} height={14} />,
            onClick: () => setAddCategoryOpen(true),
          },
        ]}
      />
    </div>
  );
}

