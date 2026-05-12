import { useState } from 'react';
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
const CATEGORY_INNER_PADDING_X = 12;
const CATEGORY_INNER_PADDING_Y = 12;
const CATEGORY_INNER_MARGIN = 24;
// Fixed cell width: cards and categories keep the same physical size no
// matter how wide the browser is. If the viewport gets narrow, the
// dashboard scrolls horizontally instead of squeezing every card.
const CELL_PX = 480;
const ROW_PX = 240;

export default function Dashboard() {
  const [addServiceOpen, setAddServiceOpen] = useState(false);
  const [addCategoryOpen, setAddCategoryOpen] = useState(false);
  const services = useServices();
  const categories = useCategories();
  const settings = useSettings();

  const cols = Number(settings.data?.grid_cols ?? DEFAULT_COLS);
  const safeCols = Number.isFinite(cols) && cols >= 4 && cols <= 12 ? cols : DEFAULT_COLS;

  const { outerLayout, flushOuter, flushInner } = useDashboardGrid(
    services.data,
    categories.data,
    safeCols,
  );

  // Grid width derived from a fixed cell size — the dashboard does not
  // shrink with the viewport, the outer container scrolls horizontally
  // when the user makes the window narrower than the grid.
  const cellPx = CELL_PX;
  const rowHeight = ROW_PX;
  const gridWidth = safeCols * CELL_PX + (safeCols - 1) * MARGIN;

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
        onContextMenu={handleBgContextMenu}
        className="min-h-[200px] overflow-x-auto"
      >
        {((services.data?.length ?? 0) > 0 || (categories.data?.length ?? 0) > 0) && (
          <GridLayout
            className="layout"
            layout={outerLayout}
            cols={safeCols}
            rowHeight={rowHeight}
            width={gridWidth}
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
              // c.layout.{w,h} are the category's *outer* dimensions in
              // cells/rows. Compute the available inner area in pixels,
              // then back out the inner cell width and row height. Inner
              // cards end up slightly smaller than outer cards (the title
              // bar + padding eat a fixed pixel budget) but a 2×2 category
              // genuinely fits 2 cols × 2 rows of cards without overflow.
              const innerCols = Math.max(1, c.layout.w);
              const innerRows = Math.max(1, c.layout.h);
              const outerW =
                innerCols * cellPx + (innerCols - 1) * MARGIN;
              const outerH = innerRows * rowHeight + (innerRows - 1) * MARGIN;
              const innerW = Math.max(80, outerW - CATEGORY_INNER_PADDING_X * 2);
              const innerH =
                outerH - CATEGORY_TITLE_PX - CATEGORY_INNER_PADDING_Y * 2;
              const innerRowHeight = Math.max(
                60,
                (innerH - CATEGORY_INNER_MARGIN * (innerRows - 1)) / innerRows,
              );
              return (
                <div key={`c-${c.id}`}>
                  <CategoryCard
                    category={c}
                    services={servicesByCategory.get(c.id) ?? []}
                    innerWidth={innerW}
                    innerCols={innerCols}
                    innerRows={innerRows}
                    innerRowHeight={innerRowHeight}
                    innerMargin={CATEGORY_INNER_MARGIN}
                    innerPaddingX={CATEGORY_INNER_PADDING_X}
                    innerPaddingY={CATEGORY_INNER_PADDING_Y}
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

