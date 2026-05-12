import { useState } from 'react';
import GridLayout, { type Layout } from 'react-grid-layout';
import { EditIcon, GripIcon, TrashIcon } from './icons';
import { ConfirmDialog } from './Modal';
import { CategoryForm } from './CategoryForm';
import { ContextMenu, useContextMenu } from './ContextMenu';
import { ServiceCard } from './ServiceCard';
import { useDeleteCategory } from '@/api/queries';
import type { Category, Service } from '@/api/types';

type Props = {
  category: Category;
  services: Service[];
  innerWidth: number;
  innerCols: number;
  innerRows: number;
  innerRowHeight: number;
  innerMargin: number;
  innerPaddingX: number;
  innerPaddingY: number;
  onInnerLayoutChange: (catID: number, layouts: Layout[]) => void;
};

export function CategoryCard({
  category,
  services,
  innerWidth,
  innerCols,
  innerRows,
  innerRowHeight,
  innerMargin,
  innerPaddingX,
  innerPaddingY,
  onInnerLayoutChange,
}: Props) {
  // Suppress unused-param lint without changing the public API.
  void innerRows;
  const [editOpen, setEditOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const del = useDeleteCategory();
  const menu = useContextMenu();

  const innerLayout: Layout[] = services.map((s) => {
    const w = Math.min(Math.max(1, s.layout.w), innerCols);
    const h = Math.max(1, s.layout.h);
    const x = Math.min(Math.max(0, s.layout.x), Math.max(0, innerCols - w));
    return {
      i: `s-${s.id}`,
      x,
      y: Math.max(0, s.layout.y),
      w,
      h,
      minW: 1,
      minH: 1,
      // Inner service cards are not resizable either; their 1×1
      // footprint inside the category matches the dashboard baseline.
      isResizable: false,
    };
  });

  return (
    <div
      className="flex h-full flex-col rounded-lg border border-border bg-bg-elevated/30 shadow-lg shadow-black/40"
      onContextMenu={menu.onContextMenu}
    >
      {/* Title bar — neutral; the category's chosen color shows as a small
          dot next to the name, so the frame stays calm and one category's
          color choice doesn't overpower the whole canvas. */}
      <div className="flex h-10 shrink-0 items-center justify-between gap-2 rounded-t-md border-b border-border bg-bg-card px-3">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <span
            className="h-2.5 w-2.5 shrink-0 rounded-full"
            style={{ background: category.border_color }}
            aria-hidden
          />
          <div className="truncate text-sm font-semibold">{category.name}</div>
        </div>
        <div
          className="rgl-drag-handle cursor-grab rounded p-1 text-fg-muted/60 hover:bg-bg-elevated hover:text-fg active:cursor-grabbing"
          title="Drag to reorder"
          onContextMenu={(e) => e.stopPropagation()}
        >
          <GripIcon width={13} height={13} />
        </div>
      </div>

      {/* Inner body */}
      <div
        className="relative flex-1"
        style={{
          paddingLeft: innerPaddingX,
          paddingRight: innerPaddingX,
          paddingTop: innerPaddingY,
          paddingBottom: innerPaddingY,
        }}
        onMouseDown={(e) => e.stopPropagation()}
      >
        {services.length === 0 ? (
          <div className="grid h-full place-items-center text-xs text-fg-muted">
            Empty — assign a service via Edit → Category dropdown.
          </div>
        ) : (
          <GridLayout
            className="layout"
            layout={innerLayout}
            cols={innerCols}
            rowHeight={innerRowHeight}
            width={innerWidth}
            margin={[innerMargin, innerMargin]}
            containerPadding={[0, 0]}
            compactType="vertical"
            preventCollision={false}
            isResizable
            isDraggable
            draggableHandle=".rgl-drag-handle"
            draggableCancel="button, a, input, select, textarea, .no-drag"
            onLayoutChange={(l) => onInnerLayoutChange(category.id, l)}
          >
            {services.map((s) => (
              <div key={`s-${s.id}`}>
                <ServiceCard service={s} />
              </div>
            ))}
          </GridLayout>
        )}
      </div>

      <CategoryForm open={editOpen} onClose={() => setEditOpen(false)} initial={category} />
      <ConfirmDialog
        open={confirmOpen}
        title="Delete category?"
        message={`Delete "${category.name}"? Services inside will be detached (not deleted).`}
        onCancel={() => setConfirmOpen(false)}
        onConfirm={async () => {
          await del.mutateAsync(category.id);
          setConfirmOpen(false);
        }}
      />

      <ContextMenu
        open={menu.open}
        x={menu.x}
        y={menu.y}
        onClose={menu.close}
        items={[
          {
            label: 'Edit category',
            icon: <EditIcon width={14} height={14} />,
            onClick: () => setEditOpen(true),
          },
          {
            label: 'Delete category',
            icon: <TrashIcon width={14} height={14} />,
            onClick: () => setConfirmOpen(true),
            danger: true,
          },
        ]}
      />
    </div>
  );
}
