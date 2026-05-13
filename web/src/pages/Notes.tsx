import { useEffect, useMemo, useRef, useState } from 'react';
import {
  useCreateNotesCard,
  useCreateNotesItem,
  useDeleteNotesCard,
  useDeleteNotesItem,
  useNotes,
  usePatchNotesCardLayout,
  useUpdateNotesCard,
  useUpdateNotesItem,
} from '@/api/queries';
import type { NotesCard, NotesItem } from '@/api/types';

// ---- Constants -----------------------------------------------------------

const MIN_ZOOM = 0.25;
const MAX_ZOOM = 3.0;
const ZOOM_STEP = 0.1;
const DEFAULT_CARD_W = 280;
const DEFAULT_CARD_H = 140;

// AABB overlap with a 1px margin so abutting edges aren't called collisions.
function overlaps(a: { x: number; y: number; w: number; h: number }, b: { x: number; y: number; w: number; h: number }): boolean {
  return !(
    a.x + a.w <= b.x + 1 ||
    b.x + b.w <= a.x + 1 ||
    a.y + a.h <= b.y + 1 ||
    b.y + b.h <= a.y + 1
  );
}

// ---- Page ----------------------------------------------------------------

export default function NotesPage() {
  const notes = useNotes(true);
  const create = useCreateNotesCard();
  const update = useUpdateNotesCard();
  const patchLayout = usePatchNotesCardLayout();
  const del = useDeleteNotesCard();
  const [search, setSearch] = useState('');

  // Canvas viewport state.
  const [zoom, setZoom] = useState(1);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const containerRef = useRef<HTMLDivElement>(null);

  // Pending position overrides so we can render dragging without waiting
  // on the server round-trip. Keyed by card id.
  const [pendingPos, setPendingPos] = useState<Record<number, { x: number; y: number }>>({});

  const cards = useMemo<NotesCard[]>(() => {
    const data = notes.data ?? [];
    return data.map((c) => (pendingPos[c.id] ? { ...c, ...pendingPos[c.id] } : c));
  }, [notes.data, pendingPos]);

  // ---- Pan + zoom handling -----------------------------------------------

  const onWheel = (e: React.WheelEvent) => {
    e.preventDefault();
    const rect = containerRef.current?.getBoundingClientRect();
    if (!rect) return;
    const mouseX = e.clientX - rect.left;
    const mouseY = e.clientY - rect.top;
    const factor = e.deltaY < 0 ? 1 + ZOOM_STEP : 1 - ZOOM_STEP;
    const newZoom = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, zoom * factor));
    if (newZoom === zoom) return;
    // Keep world point under cursor stable.
    const worldX = (mouseX - pan.x) / zoom;
    const worldY = (mouseY - pan.y) / zoom;
    setPan({ x: mouseX - worldX * newZoom, y: mouseY - worldY * newZoom });
    setZoom(newZoom);
  };

  // Background pan: only when middle mouse, or left mouse on empty canvas.
  const panState = useRef<{ startX: number; startY: number; pan0: { x: number; y: number } } | null>(null);
  const onMouseDown = (e: React.MouseEvent) => {
    if (e.button === 1 || (e.button === 0 && e.target === e.currentTarget)) {
      panState.current = { startX: e.clientX, startY: e.clientY, pan0: { ...pan } };
      e.preventDefault();
    }
  };
  useEffect(() => {
    const move = (e: MouseEvent) => {
      if (!panState.current) return;
      setPan({
        x: panState.current.pan0.x + (e.clientX - panState.current.startX),
        y: panState.current.pan0.y + (e.clientY - panState.current.startY),
      });
    };
    const up = () => {
      panState.current = null;
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
  }, []);

  // ---- Card creation + actions -------------------------------------------

  // Right-click on the background opens a tiny menu at cursor with
  // "Add card here". Coordinates are converted to world space.
  const [menu, setMenu] = useState<{ sx: number; sy: number; wx: number; wy: number } | null>(null);
  const onContextMenu = (e: React.MouseEvent) => {
    if (e.target !== e.currentTarget) return;
    e.preventDefault();
    const rect = containerRef.current?.getBoundingClientRect();
    if (!rect) return;
    const sx = e.clientX - rect.left;
    const sy = e.clientY - rect.top;
    setMenu({
      sx,
      sy,
      wx: (sx - pan.x) / zoom,
      wy: (sy - pan.y) / zoom,
    });
  };
  useEffect(() => {
    if (!menu) return;
    const close = () => setMenu(null);
    window.addEventListener('mousedown', close);
    return () => window.removeEventListener('mousedown', close);
  }, [menu]);

  const addCardAt = async (wx: number, wy: number) => {
    setMenu(null);
    try {
      await create.mutateAsync({
        name: '',
        x: wx,
        y: wy,
        w: DEFAULT_CARD_W,
        h: DEFAULT_CARD_H,
        color: '#475569',
      });
    } catch (e) {
      console.error('create card', e);
    }
  };

  // ---- Card drag ---------------------------------------------------------

  const tryMoveCard = (cardId: number, newX: number, newY: number) => {
    setPendingPos((prev) => ({ ...prev, [cardId]: { x: newX, y: newY } }));
  };

  const commitMoveCard = async (cardId: number) => {
    const current = pendingPos[cardId];
    if (!current) return;
    const card = (notes.data ?? []).find((c) => c.id === cardId);
    if (!card) return;
    const proposed = { x: current.x, y: current.y, w: card.w, h: card.h };
    const others = (notes.data ?? []).filter((c) => c.id !== cardId);
    const collision = others.some((o) =>
      overlaps(proposed, { x: o.x, y: o.y, w: o.w, h: o.h }),
    );
    if (collision) {
      // Snap back: drop the pending override so the card returns to its
      // original position on the next render.
      setPendingPos((prev) => {
        const next = { ...prev };
        delete next[cardId];
        return next;
      });
      return;
    }
    try {
      await patchLayout.mutateAsync({
        id: cardId,
        x: current.x,
        y: current.y,
        w: card.w,
        h: card.h,
      });
      // Refetch BEFORE clearing the pending override so the card doesn't
      // snap back to stale data for one render frame.
      await notes.refetch();
    } catch (e) {
      console.error('patch layout', e);
      await notes.refetch();
    } finally {
      setPendingPos((prev) => {
        const next = { ...prev };
        delete next[cardId];
        return next;
      });
    }
  };

  // ---- Search highlight --------------------------------------------------

  const q = search.trim().toLowerCase();
  const matchingIds = useMemo(() => {
    if (!q) return null;
    const out = new Set<number>();
    for (const c of cards) {
      if (c.name.toLowerCase().includes(q)) out.add(c.id);
      else if (c.items.some((i) => i.text.toLowerCase().includes(q))) out.add(c.id);
    }
    return out;
  }, [cards, q]);

  const focusCard = (cardId: number) => {
    const c = (notes.data ?? []).find((x) => x.id === cardId);
    if (!c || !containerRef.current) return;
    const rect = containerRef.current.getBoundingClientRect();
    setPan({
      x: rect.width / 2 - (c.x + c.w / 2) * zoom,
      y: rect.height / 2 - (c.y + c.h / 2) * zoom,
    });
  };

  return (
    <div className="relative h-[calc(100vh-3.5rem)] w-full overflow-hidden bg-bg">
      {/* Top toolbar */}
      <div className="absolute left-0 right-0 top-0 z-20 flex items-center gap-2 border-b border-border bg-bg-card/90 px-4 py-2 backdrop-blur">
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search cards and notes…"
          className="w-72 rounded border border-border bg-bg-elevated px-3 py-1.5 text-sm outline-none focus:border-accent"
        />
        {matchingIds && (
          <span className="text-xs text-fg-muted">
            {matchingIds.size} match{matchingIds.size === 1 ? '' : 'es'}
          </span>
        )}
        <div className="flex-1" />
        <div className="flex items-center gap-1 text-xs text-fg-muted">
          <button
            onClick={() => setZoom((z) => Math.max(MIN_ZOOM, z - 0.2))}
            className="rounded border border-border px-2 py-1 hover:bg-bg-elevated"
            aria-label="Zoom out"
          >
            −
          </button>
          <span className="w-12 text-center">{Math.round(zoom * 100)}%</span>
          <button
            onClick={() => setZoom((z) => Math.min(MAX_ZOOM, z + 0.2))}
            className="rounded border border-border px-2 py-1 hover:bg-bg-elevated"
            aria-label="Zoom in"
          >
            +
          </button>
          <button
            onClick={() => {
              setZoom(1);
              setPan({ x: 0, y: 0 });
            }}
            className="ml-2 rounded border border-border px-2 py-1 hover:bg-bg-elevated"
            title="Reset view"
          >
            reset
          </button>
        </div>
      </div>

      <div
        ref={containerRef}
        className="absolute inset-0 top-[44px] cursor-grab active:cursor-grabbing"
        style={{
          backgroundImage:
            'radial-gradient(circle, var(--border) 1px, transparent 1px)',
          backgroundSize: `${24 * zoom}px ${24 * zoom}px`,
          backgroundPosition: `${pan.x}px ${pan.y}px`,
        }}
        onWheel={onWheel}
        onMouseDown={onMouseDown}
        onContextMenu={onContextMenu}
      >
        <div
          className="absolute left-0 top-0 origin-top-left"
          style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})` }}
        >
          {cards.map((card) => (
            <CardView
              key={card.id}
              card={card}
              zoom={zoom}
              dimmed={!!q && matchingIds != null && !matchingIds.has(card.id)}
              highlighted={!!q && matchingIds != null && matchingIds.has(card.id)}
              onMove={(x, y) => tryMoveCard(card.id, x, y)}
              onMoveEnd={() => commitMoveCard(card.id)}
              onUpdate={(input) => update.mutate({ id: card.id, input })}
              onDelete={() => del.mutate(card.id)}
            />
          ))}
        </div>

        {menu && (
          <div
            className="absolute z-30 rounded border border-border bg-bg-card py-1 shadow-lg"
            style={{ left: menu.sx, top: menu.sy }}
            onMouseDown={(e) => e.stopPropagation()}
            onContextMenu={(e) => e.preventDefault()}
          >
            <button
              onClick={() => addCardAt(menu.wx, menu.wy)}
              className="block w-full px-4 py-1.5 text-left text-sm hover:bg-bg-elevated"
            >
              + Add card here
            </button>
          </div>
        )}

        {notes.isPending && (
          <div className="absolute inset-0 grid place-items-center text-fg-muted">Loading…</div>
        )}
        {!notes.isPending && cards.length === 0 && (
          <div className="absolute inset-0 grid place-items-center text-center text-fg-muted">
            <div>
              <p>Empty canvas.</p>
              <p className="mt-1 text-sm">Right-click anywhere to add your first card.</p>
            </div>
          </div>
        )}
      </div>

      {/* Search results overlay */}
      {q && matchingIds && matchingIds.size > 0 && (
        <div className="absolute left-4 top-14 z-20 max-h-72 w-72 overflow-y-auto rounded-lg border border-border bg-bg-card shadow-lg">
          {[...matchingIds].map((id) => {
            const c = cards.find((x) => x.id === id);
            if (!c) return null;
            return (
              <button
                key={id}
                onClick={() => focusCard(id)}
                className="block w-full border-b border-border px-3 py-2 text-left text-sm last:border-b-0 hover:bg-bg-elevated"
              >
                <div className="truncate font-medium">{c.name || '(untitled)'}</div>
                <div className="truncate text-xs text-fg-muted">
                  {c.items.find((i) => i.text.toLowerCase().includes(q))?.text ?? '—'}
                </div>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

// ---- CardView ------------------------------------------------------------

function CardView({
  card,
  zoom,
  dimmed,
  highlighted,
  onMove,
  onMoveEnd,
  onUpdate,
  onDelete,
}: {
  card: NotesCard;
  zoom: number;
  dimmed: boolean;
  highlighted: boolean;
  onMove: (x: number, y: number) => void;
  onMoveEnd: () => void;
  onUpdate: (input: { name: string; x: number; y: number; w: number; h: number; color: string }) => void;
  onDelete: () => void;
}) {
  const createItem = useCreateNotesItem();
  const updateItem = useUpdateNotesItem();
  const deleteItem = useDeleteNotesItem();
  const [editingName, setEditingName] = useState(false);
  const [name, setName] = useState(card.name);

  useEffect(() => setName(card.name), [card.name]);

  // Drag handling — applied to the header only.
  const dragState = useRef<{ startX: number; startY: number; cardX0: number; cardY0: number } | null>(null);
  const onHeaderMouseDown = (e: React.MouseEvent) => {
    if (e.button !== 0) return;
    if ((e.target as HTMLElement).closest('button, input')) return;
    e.preventDefault();
    dragState.current = {
      startX: e.clientX,
      startY: e.clientY,
      cardX0: card.x,
      cardY0: card.y,
    };
  };
  useEffect(() => {
    const move = (e: MouseEvent) => {
      if (!dragState.current) return;
      const dx = (e.clientX - dragState.current.startX) / zoom;
      const dy = (e.clientY - dragState.current.startY) / zoom;
      onMove(dragState.current.cardX0 + dx, dragState.current.cardY0 + dy);
    };
    const up = () => {
      if (dragState.current) {
        dragState.current = null;
        onMoveEnd();
      }
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
  }, [onMove, onMoveEnd, zoom]);

  const commitName = () => {
    setEditingName(false);
    if (name === card.name) return;
    onUpdate({
      name,
      x: card.x,
      y: card.y,
      w: card.w,
      h: card.h,
      color: card.color,
    });
  };

  const [addingItem, setAddingItem] = useState(false);
  const [newItemText, setNewItemText] = useState('');

  const addItem = async () => {
    if (!newItemText.trim()) {
      setAddingItem(false);
      setNewItemText('');
      return;
    }
    await createItem.mutateAsync({
      cardId: card.id,
      input: { text: newItemText.trim(), is_favorite: false, position: 0 },
    });
    setNewItemText('');
    setAddingItem(false);
  };

  // Track rendered card height and persist back to the server (debounced)
  // so collision detection has an accurate value.
  const cardRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = cardRef.current;
    if (!el) return;
    let timer: number | null = null;
    const ro = new ResizeObserver(() => {
      const measured = el.offsetHeight;
      if (Math.abs(measured - card.h) < 4) return;
      if (timer) window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        onUpdate({ name: card.name, x: card.x, y: card.y, w: card.w, h: measured, color: card.color });
      }, 600);
    });
    ro.observe(el);
    return () => {
      ro.disconnect();
      if (timer) window.clearTimeout(timer);
    };
    // We only want this to run on card identity / external geometry changes;
    // intentionally not depending on onUpdate.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [card.id, card.x, card.y, card.w, card.color, card.name]);

  return (
    <div
      ref={cardRef}
      className={
        'absolute rounded-lg border bg-bg-card shadow-lg transition-opacity ' +
        (highlighted ? 'ring-2 ring-accent' : '') +
        (dimmed ? ' opacity-30' : '')
      }
      style={{
        left: card.x,
        top: card.y,
        width: card.w,
        borderColor: card.color,
      }}
    >
      <div
        onMouseDown={onHeaderMouseDown}
        className="flex cursor-grab items-center gap-2 rounded-t-lg px-3 py-2 active:cursor-grabbing"
        style={{ background: `${card.color}33` }}
      >
        {editingName ? (
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={commitName}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commitName();
              if (e.key === 'Escape') {
                setName(card.name);
                setEditingName(false);
              }
            }}
            autoFocus
            className="flex-1 rounded border border-border bg-bg-elevated px-2 py-0.5 text-sm outline-none focus:border-accent"
          />
        ) : (
          <button
            onMouseDown={(e) => e.stopPropagation()}
            onClick={() => setEditingName(true)}
            className="flex-1 truncate text-left text-sm font-semibold"
          >
            {card.name || <span className="italic text-fg-muted">untitled</span>}
          </button>
        )}
        <button
          onMouseDown={(e) => e.stopPropagation()}
          onClick={() => {
            if (confirm(`Delete card "${card.name || 'untitled'}" and its items?`)) onDelete();
          }}
          className="rounded p-1 text-fg-muted hover:bg-bg-elevated hover:text-danger"
          aria-label="Delete card"
          title="Delete card"
        >
          ✕
        </button>
      </div>

      <ul className="divide-y divide-border px-1">
        {card.items.map((item) => (
          <ItemRow
            key={item.id}
            item={item}
            onUpdate={(input) => updateItem.mutate({ id: item.id, input })}
            onDelete={() => deleteItem.mutate(item.id)}
          />
        ))}
        {addingItem && (
          <li className="flex items-center gap-1 px-2 py-1.5">
            <input
              value={newItemText}
              onChange={(e) => setNewItemText(e.target.value)}
              onBlur={addItem}
              onKeyDown={(e) => {
                if (e.key === 'Enter') addItem();
                if (e.key === 'Escape') {
                  setAddingItem(false);
                  setNewItemText('');
                }
              }}
              placeholder="Note text…"
              autoFocus
              className="flex-1 rounded border border-border bg-bg-elevated px-2 py-1 text-xs outline-none focus:border-accent"
            />
          </li>
        )}
      </ul>

      <div className="px-2 pb-2 pt-1">
        <button
          onClick={() => setAddingItem(true)}
          className="w-full rounded border border-dashed border-border px-2 py-1 text-xs text-fg-muted hover:border-accent hover:text-fg"
        >
          + Add note
        </button>
      </div>
    </div>
  );
}

// ---- ItemRow -------------------------------------------------------------

function ItemRow({
  item,
  onUpdate,
  onDelete,
}: {
  item: NotesItem;
  onUpdate: (input: { text: string; is_favorite: boolean; position: number }) => void;
  onDelete: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(item.text);
  useEffect(() => setText(item.text), [item.text]);

  const commit = () => {
    setEditing(false);
    if (text === item.text) return;
    onUpdate({ text, is_favorite: item.is_favorite, position: item.position });
  };

  return (
    <li className="flex items-center gap-1 px-2 py-1 text-xs">
      <button
        onClick={() =>
          onUpdate({ text: item.text, is_favorite: !item.is_favorite, position: item.position })
        }
        className={
          'shrink-0 rounded p-1 hover:bg-bg-elevated ' +
          (item.is_favorite ? 'text-warning' : 'text-fg-muted/50 hover:text-fg-muted')
        }
        title={item.is_favorite ? 'Remove from favourites' : 'Mark as favourite'}
        aria-label="Toggle favourite"
      >
        {item.is_favorite ? '★' : '☆'}
      </button>
      {editing ? (
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === 'Enter') commit();
            if (e.key === 'Escape') {
              setText(item.text);
              setEditing(false);
            }
          }}
          autoFocus
          className="flex-1 rounded border border-border bg-bg-elevated px-2 py-0.5 text-xs outline-none focus:border-accent"
        />
      ) : (
        <button
          onClick={() => setEditing(true)}
          className="flex-1 truncate text-left hover:text-fg"
          title={item.text}
        >
          {item.text || <span className="italic text-fg-muted">empty</span>}
        </button>
      )}
      <button
        onClick={() => {
          if (confirm('Delete this note?')) onDelete();
        }}
        className="shrink-0 rounded p-1 text-fg-muted/50 hover:bg-bg-elevated hover:text-danger"
        aria-label="Delete note"
        title="Delete"
      >
        ✕
      </button>
    </li>
  );
}

