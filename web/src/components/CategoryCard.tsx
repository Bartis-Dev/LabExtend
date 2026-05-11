import { useState } from 'react';
import GridLayout, { type Layout } from 'react-grid-layout';
import { EditIcon, TrashIcon } from './icons';
import { ConfirmDialog } from './Modal';
import { CategoryForm } from './CategoryForm';
import { ServiceCard } from './ServiceCard';
import { useDeleteCategory } from '@/api/queries';
import type { Category, Service } from '@/api/types';

type Props = {
  category: Category;
  services: Service[];
  innerWidth: number;
  innerCols: number;
  innerRowHeight: number;
  innerMargin: number;
  innerPadding: number;
  onInnerLayoutChange: (catID: number, layouts: Layout[]) => void;
};

export function CategoryCard({
  category,
  services,
  innerWidth,
  innerCols,
  innerRowHeight,
  innerMargin,
  innerPadding,
  onInnerLayoutChange,
}: Props) {
  const [editOpen, setEditOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const del = useDeleteCategory();

  // Defensive clamp for inner items.
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
    };
  });

  return (
    <div
      className="flex h-full flex-col overflow-hidden rounded-lg border-2 bg-bg-card/40"
      style={{ borderColor: category.border_color }}
    >
      {/* Title bar — only this is a drag handle for the outer grid. */}
      <div
        className="rgl-outer-drag flex h-9 shrink-0 cursor-move items-center justify-between gap-2 border-b px-3"
        style={{ borderColor: category.border_color }}
      >
        <div className="flex-1 truncate text-sm font-semibold">{category.name}</div>
        <div className="flex items-center gap-0.5">
          <button
            onClick={(e) => {
              e.stopPropagation();
              setEditOpen(true);
            }}
            onMouseDown={(e) => e.stopPropagation()}
            className="no-drag rounded p-1 text-fg-muted hover:bg-bg-elevated hover:text-fg"
            aria-label="Edit category"
          >
            <EditIcon width={13} height={13} />
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation();
              setConfirmOpen(true);
            }}
            onMouseDown={(e) => e.stopPropagation()}
            className="no-drag rounded p-1 text-fg-muted hover:bg-bg-elevated hover:text-danger"
            aria-label="Delete category"
          >
            <TrashIcon width={13} height={13} />
          </button>
        </div>
      </div>

      {/* Inner body — NOT a drag handle. Service cards inside get their own
          inner grid for repositioning. Padding gives breathing room between
          frame and inner cards. */}
      <div
        className="no-drag flex-1 overflow-hidden"
        style={{ padding: innerPadding }}
        onMouseDown={(e) => e.stopPropagation()}
      >
        {services.length === 0 ? (
          <div className="grid h-full place-items-center text-xs text-fg-muted">
            Empty — add services here via their form or drag one in.
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
            compactType={null}
            preventCollision={false}
            isResizable
            isDraggable
            draggableCancel="button, a, input, select, textarea"
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
    </div>
  );
}
