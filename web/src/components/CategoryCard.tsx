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
  cellPx: number;
  // Inner-grid width and column count are derived from outer width and category.layout.w
  innerWidth: number;
  innerCols: number;
  onInnerLayoutChange: (catID: number, layouts: Layout[]) => void;
};

export function CategoryCard({
  category,
  services,
  cellPx,
  innerWidth,
  innerCols,
  onInnerLayoutChange,
}: Props) {
  const [editOpen, setEditOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const del = useDeleteCategory();

  // Derive the inner GridLayout layout array from the services in this category.
  // Defensive clamps: width never exceeds innerCols; positions never escape the box.
  const innerLayout: Layout[] = services.map((s) => {
    const w = Math.min(s.layout.w, innerCols);
    const x = Math.min(s.layout.x, Math.max(0, innerCols - w));
    return {
      i: `s-${s.id}`,
      x,
      y: s.layout.y,
      w,
      h: Math.max(1, s.layout.h),
      minW: 1,
      minH: 1,
    };
  });

  return (
    <div
      className="flex h-full flex-col overflow-hidden rounded-lg border-2 bg-bg-card/60"
      style={{ borderColor: category.border_color }}
    >
      <div
        className="flex items-center justify-between gap-2 border-b px-3 py-1.5"
        style={{ borderColor: category.border_color }}
      >
        <div className="cat-drag-handle flex-1 cursor-move truncate text-sm font-semibold">
          {category.name}
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={(e) => {
              e.stopPropagation();
              setEditOpen(true);
            }}
            onMouseDown={(e) => e.stopPropagation()}
            className="rounded p-1 text-fg-muted hover:bg-bg-elevated hover:text-fg"
            aria-label="Edit category"
          >
            <EditIcon width={14} height={14} />
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation();
              setConfirmOpen(true);
            }}
            onMouseDown={(e) => e.stopPropagation()}
            className="rounded p-1 text-fg-muted hover:bg-bg-elevated hover:text-danger"
            aria-label="Delete category"
          >
            <TrashIcon width={14} height={14} />
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-hidden p-1">
        {services.length === 0 ? (
          <div className="grid h-full place-items-center text-xs text-fg-muted">
            Empty — assign a service via its edit form.
          </div>
        ) : (
          <GridLayout
            className="layout"
            layout={innerLayout}
            cols={innerCols}
            rowHeight={cellPx}
            width={innerWidth}
            margin={[6, 6]}
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
