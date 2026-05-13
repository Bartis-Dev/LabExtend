import { useEffect, useMemo, useRef, useState } from 'react';
import { ContextMenu, type ContextMenuItem, useContextMenu } from '@/components/ContextMenu';
import {
  useCreateNotesBoard,
  useCreateNotesCard,
  useCreateNotesItem,
  useDeleteNotesBoard,
  useDeleteNotesCard,
  useDeleteNotesItem,
  useMoveNotesItem,
  useNotes,
  usePatchNotesBoardPosition,
  usePatchNotesCardLayout,
  useSwapNotesCardSlots,
  useUpdateNotesBoard,
  useUpdateNotesCard,
  useUpdateNotesItem,
} from '@/api/queries';
import type { NotesBoard, NotesCard, NotesItem } from '@/api/types';

// ---- Tunables -----------------------------------------------------------

const MIN_ZOOM = 0.25;
const MAX_ZOOM = 3.0;
const ZOOM_STEP = 0.1;
const DEFAULT_CARD_W = 280;
const DEFAULT_CARD_H = 140;

const BOARD_HEADER_H = 36;
const BOARD_INNER_PAD = 12;
const BOARD_INNER_GAP = 16;

// MIME type used by the item drag-and-drop. The string itself is
// arbitrary — the only contract is "no other widget in this app uses it".
const ITEM_DRAG_MIME = 'application/x-labextend-note-item';

// AABB overlap with a 1px margin so abutting edges aren't called collisions.
function overlaps(
  a: { x: number; y: number; w: number; h: number },
  b: { x: number; y: number; w: number; h: number },
): boolean {
  return !(
    a.x + a.w <= b.x + 1 ||
    b.x + b.w <= a.x + 1 ||
    a.y + a.h <= b.y + 1 ||
    b.y + b.h <= a.y + 1
  );
}

function findFreeSpot(
  wx: number,
  wy: number,
  w: number,
  h: number,
  others: { x: number; y: number; w: number; h: number }[],
): { x: number; y: number } {
  const gap = 16;
  const stepX = w + gap;
  const stepY = h + gap;
  const free = (x: number, y: number) =>
    !others.some((o) => overlaps({ x, y, w, h }, o));
  if (free(wx, wy)) return { x: wx, y: wy };
  for (let ring = 1; ring <= 12; ring++) {
    const candidates: Array<[number, number]> = [];
    for (let i = -ring; i <= ring; i++) {
      candidates.push([wx + i * stepX, wy - ring * stepY]);
      candidates.push([wx + i * stepX, wy + ring * stepY]);
    }
    for (let j = -ring + 1; j < ring; j++) {
      candidates.push([wx - ring * stepX, wy + j * stepY]);
      candidates.push([wx + ring * stepX, wy + j * stepY]);
    }
    for (const [cx, cy] of candidates) {
      if (free(cx, cy)) return { x: cx, y: cy };
    }
  }
  const maxY = others.reduce((m, o) => Math.max(m, o.y + o.h), 0);
  return { x: wx, y: maxY + gap };
}

function boardWidth(cols: number): number {
  return cols * DEFAULT_CARD_W + (cols - 1) * BOARD_INNER_GAP + 2 * BOARD_INNER_PAD;
}

// ---- Page --------------------------------------------------------------

export default function NotesPage() {
  const notes = useNotes(true);
  const create = useCreateNotesCard();
  const update = useUpdateNotesCard();
  const patchLayout = usePatchNotesCardLayout();
  const del = useDeleteNotesCard();
  const createBoard = useCreateNotesBoard();
  const [search, setSearch] = useState('');

  const [zoom, setZoom] = useState(1);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const containerRef = useRef<HTMLDivElement>(null);

  // Pending position overrides while dragging a free card or board.
  const [pendingCardPos, setPendingCardPos] = useState<Record<number, { x: number; y: number }>>({});
  const [pendingBoardPos, setPendingBoardPos] = useState<Record<number, { x: number; y: number }>>({});

  const allCards = useMemo<NotesCard[]>(() => {
    const data = notes.data?.cards ?? [];
    return data.map((c) =>
      pendingCardPos[c.id] ? { ...c, ...pendingCardPos[c.id] } : c,
    );
  }, [notes.data, pendingCardPos]);

  const boards = useMemo<NotesBoard[]>(() => {
    const data = notes.data?.boards ?? [];
    return data.map((b) =>
      pendingBoardPos[b.id] ? { ...b, ...pendingBoardPos[b.id] } : b,
    );
  }, [notes.data, pendingBoardPos]);

  const freeCards = useMemo(() => allCards.filter((c) => c.board_id == null), [allCards]);
  const cardsByBoard = useMemo(() => {
    const m = new Map<number, NotesCard[]>();
    for (const c of allCards) {
      if (c.board_id != null) {
        const arr = m.get(c.board_id) ?? [];
        arr.push(c);
        m.set(c.board_id, arr);
      }
    }
    for (const [, arr] of m) arr.sort((a, b) => a.slot_index - b.slot_index);
    return m;
  }, [allCards]);

  // ---- Pan + zoom -------------------------------------------------------
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const handler = (e: WheelEvent) => {
      e.preventDefault();
      const rect = el.getBoundingClientRect();
      const mouseX = e.clientX - rect.left;
      const mouseY = e.clientY - rect.top;
      const factor = e.deltaY < 0 ? 1 + ZOOM_STEP : 1 - ZOOM_STEP;
      setZoom((z) => {
        const nz = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, z * factor));
        if (nz === z) return z;
        setPan((p) => {
          const worldX = (mouseX - p.x) / z;
          const worldY = (mouseY - p.y) / z;
          return { x: mouseX - worldX * nz, y: mouseY - worldY * nz };
        });
        return nz;
      });
    };
    el.addEventListener('wheel', handler, { passive: false });
    return () => el.removeEventListener('wheel', handler);
  }, []);

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

  // ---- Canvas right-click menu (add note / create canvas) ----------------
  const canvasMenu = useContextMenu();
  const [menuWorld, setMenuWorld] = useState<{ wx: number; wy: number } | null>(null);
  const onContextMenu = (e: React.MouseEvent) => {
    const rect = containerRef.current?.getBoundingClientRect();
    if (!rect) return;
    const sx = e.clientX - rect.left;
    const sy = e.clientY - rect.top;
    setMenuWorld({ wx: (sx - pan.x) / zoom, wy: (sy - pan.y) / zoom });
    canvasMenu.onContextMenu(e);
  };

  const addCardAt = async (wx: number, wy: number) => {
    const others = freeCards.map((c) => ({ x: c.x, y: c.y, w: c.w, h: c.h }));
    // Boards also occupy free-canvas space; include them in the collision set.
    for (const b of boards) {
      const cards = cardsByBoard.get(b.id) ?? [];
      const h = Math.max(...cards.map((c) => c.h), DEFAULT_CARD_H);
      others.push({ x: b.x, y: b.y, w: boardWidth(b.cols), h: h + BOARD_HEADER_H + BOARD_INNER_PAD * 2 });
    }
    const spot = findFreeSpot(wx, wy, DEFAULT_CARD_W, DEFAULT_CARD_H, others);
    try {
      await create.mutateAsync({
        name: '',
        x: spot.x,
        y: spot.y,
        w: DEFAULT_CARD_W,
        h: DEFAULT_CARD_H,
        color: '#475569',
      });
    } catch (e) {
      console.error('create card', e);
    }
  };

  const createCanvasBoard = async (cols: number, wx: number, wy: number) => {
    try {
      await createBoard.mutateAsync({
        name: '',
        x: wx,
        y: wy,
        cols,
        color: '#475569',
      });
    } catch (e) {
      console.error('create board', e);
    }
  };

  const canvasMenuItems: ContextMenuItem[] = menuWorld
    ? [
        { label: 'Add note here', onClick: () => addCardAt(menuWorld.wx, menuWorld.wy) },
        { separator: true },
        ...([2, 3, 4, 5] as const).map(
          (n): ContextMenuItem => ({
            label: `Create canvas with ${n} cards`,
            onClick: () => createCanvasBoard(n, menuWorld.wx, menuWorld.wy),
          }),
        ),
      ]
    : [];

  // ---- Card move (free canvas) ------------------------------------------
  const tryMoveCard = (cardId: number, nx: number, ny: number) =>
    setPendingCardPos((p) => ({ ...p, [cardId]: { x: nx, y: ny } }));
  const commitMoveCard = async (cardId: number) => {
    const cur = pendingCardPos[cardId];
    if (!cur) return;
    const card = (notes.data?.cards ?? []).find((c) => c.id === cardId);
    if (!card) return;
    const proposed = { x: cur.x, y: cur.y, w: card.w, h: card.h };
    const others = (notes.data?.cards ?? [])
      .filter((c) => c.id !== cardId && c.board_id == null)
      .map((c) => ({ x: c.x, y: c.y, w: c.w, h: c.h }));
    for (const b of boards) {
      const inb = cardsByBoard.get(b.id) ?? [];
      const h = Math.max(...inb.map((c) => c.h), DEFAULT_CARD_H);
      others.push({ x: b.x, y: b.y, w: boardWidth(b.cols), h: h + BOARD_HEADER_H + BOARD_INNER_PAD * 2 });
    }
    if (others.some((o) => overlaps(proposed, o))) {
      setPendingCardPos((p) => {
        const next = { ...p };
        delete next[cardId];
        return next;
      });
      return;
    }
    try {
      await patchLayout.mutateAsync({ id: cardId, x: cur.x, y: cur.y, w: card.w, h: card.h });
      await notes.refetch();
    } catch (e) {
      console.error(e);
      await notes.refetch();
    } finally {
      setPendingCardPos((p) => {
        const next = { ...p };
        delete next[cardId];
        return next;
      });
    }
  };

  // ---- Board move -------------------------------------------------------
  const patchBoardPos = usePatchNotesBoardPosition();
  const tryMoveBoard = (id: number, nx: number, ny: number) =>
    setPendingBoardPos((p) => ({ ...p, [id]: { x: nx, y: ny } }));
  const commitMoveBoard = async (id: number) => {
    const cur = pendingBoardPos[id];
    if (!cur) return;
    try {
      await patchBoardPos.mutateAsync({ id, x: cur.x, y: cur.y });
      await notes.refetch();
    } catch (e) {
      console.error(e);
      await notes.refetch();
    } finally {
      setPendingBoardPos((p) => {
        const next = { ...p };
        delete next[id];
        return next;
      });
    }
  };

  // ---- Search ------------------------------------------------------------
  const q = search.trim().toLowerCase();
  const matchingIds = useMemo(() => {
    if (!q) return null;
    const out = new Set<number>();
    for (const c of allCards) {
      if (c.name.toLowerCase().includes(q)) out.add(c.id);
      else if (c.items.some((i) => i.text.toLowerCase().includes(q))) out.add(c.id);
    }
    return out;
  }, [allCards, q]);

  const focusCard = (cardId: number) => {
    const c = (notes.data?.cards ?? []).find((x) => x.id === cardId);
    if (!c || !containerRef.current) return;
    const rect = containerRef.current.getBoundingClientRect();
    setPan({
      x: rect.width / 2 - (c.x + c.w / 2) * zoom,
      y: rect.height / 2 - (c.y + c.h / 2) * zoom,
    });
  };

  return (
    <div className="relative h-full w-full overflow-hidden bg-bg">
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
        onMouseDown={onMouseDown}
        onContextMenu={onContextMenu}
      >
        <div
          className="absolute left-0 top-0 origin-top-left"
          style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})` }}
        >
          {boards.map((b) => (
            <BoardView
              key={b.id}
              board={b}
              cards={cardsByBoard.get(b.id) ?? []}
              zoom={zoom}
              dimmedSet={q && matchingIds ? matchingIds : null}
              onMove={(x, y) => tryMoveBoard(b.id, x, y)}
              onMoveEnd={() => commitMoveBoard(b.id)}
            />
          ))}
          {freeCards.map((card) => (
            <CardView
              key={card.id}
              card={card}
              mode="free"
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

        <ContextMenu
          open={canvasMenu.open}
          x={canvasMenu.x}
          y={canvasMenu.y}
          items={canvasMenuItems}
          onClose={canvasMenu.close}
        />

        {notes.isPending && (
          <div className="absolute inset-0 grid place-items-center text-fg-muted">Loading…</div>
        )}
        {!notes.isPending && allCards.length === 0 && boards.length === 0 && (
          <div className="absolute inset-0 grid place-items-center text-center text-fg-muted">
            <div>
              <p>Empty canvas.</p>
              <p className="mt-1 text-sm">
                Right-click to add a note or create a canvas of 2–5 cards.
              </p>
            </div>
          </div>
        )}
      </div>

      {q && matchingIds && matchingIds.size > 0 && (
        <div className="absolute left-4 top-14 z-20 max-h-72 w-72 overflow-y-auto rounded-lg border border-border bg-bg-card shadow-lg">
          {[...matchingIds].map((id) => {
            const c = allCards.find((x) => x.id === id);
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

// ---- BoardView ---------------------------------------------------------

function BoardView({
  board,
  cards,
  zoom,
  dimmedSet,
  onMove,
  onMoveEnd,
}: {
  board: NotesBoard;
  cards: NotesCard[];
  zoom: number;
  dimmedSet: Set<number> | null;
  onMove: (x: number, y: number) => void;
  onMoveEnd: () => void;
}) {
  const update = useUpdateNotesBoard();
  const del = useDeleteNotesBoard();
  const swap = useSwapNotesCardSlots();
  const updateCard = useUpdateNotesCard();
  const delCard = useDeleteNotesCard();
  const ctx = useContextMenu();
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(board.name);
  const cardListRef = useRef<HTMLDivElement>(null);

  useEffect(() => setName(board.name), [board.name]);

  // ---- Board drag (move on free canvas) ---------------------------------
  const dragState = useRef<{ sx: number; sy: number; x0: number; y0: number } | null>(null);
  const onHeaderMouseDown = (e: React.MouseEvent) => {
    if (e.button !== 0) return;
    if ((e.target as HTMLElement).closest('button, input')) return;
    e.preventDefault();
    e.stopPropagation();
    dragState.current = { sx: e.clientX, sy: e.clientY, x0: board.x, y0: board.y };
  };
  useEffect(() => {
    const move = (e: MouseEvent) => {
      if (!dragState.current) return;
      const dx = (e.clientX - dragState.current.sx) / zoom;
      const dy = (e.clientY - dragState.current.sy) / zoom;
      onMove(dragState.current.x0 + dx, dragState.current.y0 + dy);
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
  }, [zoom, onMove, onMoveEnd]);

  // ---- In-board card slot drag (swap) ------------------------------------
  const [draggingSlot, setDraggingSlot] = useState<{
    cardId: number;
    fromSlot: number;
    hoverSlot: number;
  } | null>(null);

  const startSlotDrag = (cardId: number, fromSlot: number) => {
    setDraggingSlot({ cardId, fromSlot, hoverSlot: fromSlot });
  };
  useEffect(() => {
    if (!draggingSlot) return;
    const move = (e: MouseEvent) => {
      const rect = cardListRef.current?.getBoundingClientRect();
      if (!rect) return;
      // Hover slot determined by cursor X within the card-row content area.
      const localX = (e.clientX - rect.left) / zoom;
      const slot = Math.max(
        0,
        Math.min(
          board.cols - 1,
          Math.floor(localX / (DEFAULT_CARD_W + BOARD_INNER_GAP)),
        ),
      );
      // Only honour the hover if the cursor is also vertically inside the
      // board — leaving the board cancels the drop.
      const insideY = e.clientY >= rect.top && e.clientY <= rect.bottom;
      const insideX = e.clientX >= rect.left && e.clientX <= rect.right;
      setDraggingSlot((d) =>
        d && insideX && insideY ? { ...d, hoverSlot: slot } : d && { ...d, hoverSlot: d.fromSlot },
      );
    };
    const up = () => {
      const d = draggingSlot;
      setDraggingSlot(null);
      if (!d || d.hoverSlot === d.fromSlot) return;
      const target = cards.find((c) => c.slot_index === d.hoverSlot);
      if (!target) return;
      swap.mutate({ a: d.cardId, b: target.id });
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
  }, [draggingSlot, zoom, board.cols, cards, swap]);

  // ---- Right-click menu --------------------------------------------------
  const commitRename = () => {
    setRenaming(false);
    if (name === board.name) return;
    update.mutate({
      id: board.id,
      input: { name, x: board.x, y: board.y, cols: board.cols, color: board.color },
    });
  };

  const menuItems: ContextMenuItem[] = [
    { label: 'Rename board', onClick: () => setRenaming(true) },
    { separator: true },
    {
      label: 'Delete board (with all cards)',
      danger: true,
      onClick: () => {
        if (confirm(`Delete board "${board.name || 'untitled'}" and all its cards?`)) {
          del.mutate(board.id);
        }
      },
    },
  ];

  const width = boardWidth(board.cols);

  return (
    <div
      className="absolute rounded-xl border bg-bg-card/30 backdrop-blur"
      style={{
        left: board.x,
        top: board.y,
        width,
        borderColor: board.color,
        boxShadow: '0 0 0 1px var(--border)',
      }}
      onContextMenu={(e) => {
        // Only fire when the user right-clicks the board chrome (header
        // bar or padding around cards), NOT when they right-click a card
        // inside the board. The card stops propagation on its own.
        ctx.onContextMenu(e);
      }}
    >
      <div
        onMouseDown={onHeaderMouseDown}
        className="flex h-9 cursor-grab items-center gap-2 rounded-t-xl px-3 active:cursor-grabbing"
        style={{ background: `${board.color}33` }}
      >
        {renaming ? (
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commitRename();
              if (e.key === 'Escape') {
                setName(board.name);
                setRenaming(false);
              }
            }}
            autoFocus
            onMouseDown={(e) => e.stopPropagation()}
            className="flex-1 rounded border border-border bg-bg-elevated px-2 py-0.5 text-sm outline-none focus:border-accent"
          />
        ) : (
          <span className="flex-1 truncate text-sm font-semibold">
            {board.name || <span className="italic text-fg-muted">canvas</span>}
            <span className="ml-2 text-[10px] uppercase tracking-wider text-fg-muted/70">
              {board.cols} cards
            </span>
          </span>
        )}
      </div>

      <div
        ref={cardListRef}
        className="flex"
        style={{
          padding: BOARD_INNER_PAD,
          gap: BOARD_INNER_GAP,
        }}
      >
        {cards.map((c) => {
          const dim = dimmedSet != null && !dimmedSet.has(c.id);
          const highlight = dimmedSet != null && dimmedSet.has(c.id);
          const isDraggedSource = draggingSlot?.cardId === c.id;
          const isDropTarget =
            draggingSlot != null &&
            draggingSlot.cardId !== c.id &&
            draggingSlot.hoverSlot === c.slot_index;
          return (
            <div
              key={c.id}
              className="relative"
              style={{ width: DEFAULT_CARD_W }}
            >
              {isDropTarget && (
                <div className="pointer-events-none absolute inset-0 z-10 rounded-lg border-2 border-dashed border-accent" />
              )}
              <CardView
                card={c}
                mode="inboard"
                zoom={zoom}
                dimmed={dim || (isDraggedSource && draggingSlot != null)}
                highlighted={highlight}
                onSlotDragStart={() => startSlotDrag(c.id, c.slot_index)}
                onUpdate={(input) => updateCard.mutate({ id: c.id, input })}
                onDelete={() => delCard.mutate(c.id)}
              />
            </div>
          );
        })}
      </div>

      <ContextMenu open={ctx.open} x={ctx.x} y={ctx.y} items={menuItems} onClose={ctx.close} />
    </div>
  );
}

// ---- CardView ----------------------------------------------------------

type CardMode = 'free' | 'inboard';

function CardView({
  card,
  mode,
  zoom,
  dimmed,
  highlighted,
  onMove,
  onMoveEnd,
  onSlotDragStart,
  onUpdate,
  onDelete,
}: {
  card: NotesCard;
  mode: CardMode;
  zoom: number;
  dimmed: boolean;
  highlighted: boolean;
  onMove?: (x: number, y: number) => void;
  onMoveEnd?: () => void;
  onSlotDragStart?: () => void;
  onUpdate: (input: {
    name: string;
    x: number;
    y: number;
    w: number;
    h: number;
    color: string;
    board_id?: number | null;
    slot_index?: number;
  }) => void;
  onDelete: () => void;
}) {
  const createItem = useCreateNotesItem();
  const updateItem = useUpdateNotesItem();
  const deleteItem = useDeleteNotesItem();
  const moveItem = useMoveNotesItem();
  const ctx = useContextMenu();
  const cardRef = useRef<HTMLDivElement>(null);

  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(card.name);
  useEffect(() => setName(card.name), [card.name]);

  // ---- Drag handling — free vs in-board switch --------------------------
  const dragState = useRef<{ sx: number; sy: number; x0: number; y0: number } | null>(null);
  const onHeaderMouseDown = (e: React.MouseEvent) => {
    if (e.button !== 0) return;
    if ((e.target as HTMLElement).closest('button, input')) return;
    e.preventDefault();
    e.stopPropagation();
    if (mode === 'inboard' && onSlotDragStart) {
      onSlotDragStart();
      return;
    }
    if (mode === 'free') {
      dragState.current = { sx: e.clientX, sy: e.clientY, x0: card.x, y0: card.y };
    }
  };
  useEffect(() => {
    if (mode !== 'free') return;
    const move = (e: MouseEvent) => {
      if (!dragState.current || !onMove) return;
      const dx = (e.clientX - dragState.current.sx) / zoom;
      const dy = (e.clientY - dragState.current.sy) / zoom;
      onMove(dragState.current.x0 + dx, dragState.current.y0 + dy);
    };
    const up = () => {
      if (dragState.current) {
        dragState.current = null;
        onMoveEnd?.();
      }
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
  }, [mode, onMove, onMoveEnd, zoom]);

  const commitName = () => {
    setRenaming(false);
    if (name === card.name) return;
    onUpdate({
      name,
      x: card.x,
      y: card.y,
      w: card.w,
      h: card.h,
      color: card.color,
      board_id: card.board_id,
      slot_index: card.slot_index,
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

  // Track rendered height so the server-side W/H stays accurate for
  // free-card collision detection. Only useful for free cards.
  useEffect(() => {
    if (mode !== 'free') return;
    const el = cardRef.current;
    if (!el) return;
    let timer: number | null = null;
    const ro = new ResizeObserver(() => {
      const measured = el.offsetHeight;
      if (Math.abs(measured - card.h) < 4) return;
      if (timer) window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        onUpdate({
          name: card.name,
          x: card.x,
          y: card.y,
          w: card.w,
          h: measured,
          color: card.color,
          board_id: card.board_id,
          slot_index: card.slot_index,
        });
      }, 600);
    });
    ro.observe(el);
    return () => {
      ro.disconnect();
      if (timer) window.clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [card.id, card.x, card.y, card.w, card.color, card.name, mode]);

  // ---- Right-click menu for the card --------------------------------------
  const menuItems: ContextMenuItem[] = [
    { label: 'Rename', onClick: () => setRenaming(true) },
    { label: 'Add note', onClick: () => setAddingItem(true) },
    { separator: true },
    {
      label: 'Delete card',
      danger: true,
      onClick: () => {
        if (confirm(`Delete card "${card.name || 'untitled'}" and its notes?`)) {
          onDelete();
        }
      },
    },
  ];

  // ---- Drop target on the card body for items appended to end -----------
  const [dropAtEnd, setDropAtEnd] = useState(false);

  const cardPositionStyle =
    mode === 'free'
      ? { left: card.x, top: card.y, width: card.w }
      : { position: 'relative' as const, width: card.w };

  return (
    <div
      ref={cardRef}
      onContextMenu={(e) => {
        ctx.onContextMenu(e);
      }}
      className={
        (mode === 'free' ? 'absolute ' : '') +
        'rounded-lg border bg-bg-card shadow-lg transition-opacity ' +
        (highlighted ? 'ring-2 ring-accent' : '') +
        (dimmed ? ' opacity-30' : '')
      }
      style={{
        ...cardPositionStyle,
        borderColor: card.color,
      }}
    >
      <div
        onMouseDown={onHeaderMouseDown}
        className="flex cursor-grab items-center gap-2 rounded-t-lg px-3 py-2 active:cursor-grabbing"
        style={{ background: `${card.color}33` }}
        title="Drag the title bar to move • right-click for actions"
      >
        {renaming ? (
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={commitName}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commitName();
              if (e.key === 'Escape') {
                setName(card.name);
                setRenaming(false);
              }
            }}
            autoFocus
            onMouseDown={(e) => e.stopPropagation()}
            className="flex-1 rounded border border-border bg-bg-elevated px-2 py-0.5 text-sm outline-none focus:border-accent"
          />
        ) : (
          <span className="flex-1 truncate text-sm font-semibold select-none">
            {card.name || <span className="italic text-fg-muted">untitled</span>}
          </span>
        )}
      </div>

      <ul
        className={
          'divide-y divide-border px-1 transition-colors ' +
          (dropAtEnd ? 'bg-accent/5' : '')
        }
        onDragOver={(e) => {
          // Allow dropping in the empty space of the card.
          if (!hasItemDrag(e)) return;
          e.preventDefault();
          if (e.target === e.currentTarget) {
            e.dataTransfer.dropEffect = 'move';
            setDropAtEnd(true);
          }
        }}
        onDragLeave={(e) => {
          if (e.target === e.currentTarget) setDropAtEnd(false);
        }}
        onDrop={(e) => {
          if (!hasItemDrag(e)) return;
          e.preventDefault();
          setDropAtEnd(false);
          const itemID = Number(e.dataTransfer.getData(ITEM_DRAG_MIME));
          if (!itemID) return;
          // Append to end: position = current count (the item count after
          // removing the source if it was in this card is handled
          // server-side by MoveItem).
          moveItem.mutate({ id: itemID, card_id: card.id, position: card.items.length });
        }}
      >
        {card.items.map((item) => (
          <ItemRow
            key={item.id}
            item={item}
            destCardID={card.id}
            onUpdate={(input) => updateItem.mutate({ id: item.id, input })}
            onDelete={() => deleteItem.mutate(item.id)}
            onDropAbove={(droppedItemID) =>
              moveItem.mutate({
                id: droppedItemID,
                card_id: card.id,
                position: item.position,
              })
            }
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

      <ContextMenu open={ctx.open} x={ctx.x} y={ctx.y} items={menuItems} onClose={ctx.close} />
    </div>
  );
}

function hasItemDrag(e: React.DragEvent): boolean {
  return e.dataTransfer.types.includes(ITEM_DRAG_MIME);
}

// ---- ItemRow ----------------------------------------------------------

function ItemRow({
  item,
  destCardID,
  onUpdate,
  onDelete,
  onDropAbove,
}: {
  item: NotesItem;
  destCardID: number;
  onUpdate: (input: { text: string; is_favorite: boolean; position: number }) => void;
  onDelete: () => void;
  onDropAbove: (droppedItemID: number) => void;
}) {
  const ctx = useContextMenu();
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(item.text);
  const [dragOverHere, setDragOverHere] = useState(false);
  const [isDragging, setIsDragging] = useState(false);
  useEffect(() => setText(item.text), [item.text]);

  const commit = () => {
    setEditing(false);
    if (text === item.text) return;
    onUpdate({ text, is_favorite: item.is_favorite, position: item.position });
  };

  const menuItems: ContextMenuItem[] = [
    { label: 'Edit text', onClick: () => setEditing(true) },
    {
      label: item.is_favorite ? 'Remove from favourites' : 'Mark as favourite',
      onClick: () =>
        onUpdate({ text: item.text, is_favorite: !item.is_favorite, position: item.position }),
    },
    { separator: true },
    {
      label: 'Delete note',
      danger: true,
      onClick: () => {
        if (confirm('Delete this note?')) onDelete();
      },
    },
  ];

  // Void unused so TS doesn't complain about destCardID — it's part of
  // the parent contract (onDropAbove embeds the card id) but not used
  // directly in this component.
  void destCardID;

  return (
    <li
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData(ITEM_DRAG_MIME, String(item.id));
        e.dataTransfer.effectAllowed = 'move';
        setIsDragging(true);
      }}
      onDragEnd={() => setIsDragging(false)}
      onDragOver={(e) => {
        if (!hasItemDrag(e)) return;
        e.preventDefault();
        e.stopPropagation();
        e.dataTransfer.dropEffect = 'move';
        setDragOverHere(true);
      }}
      onDragLeave={() => setDragOverHere(false)}
      onDrop={(e) => {
        if (!hasItemDrag(e)) return;
        e.preventDefault();
        e.stopPropagation();
        setDragOverHere(false);
        const droppedID = Number(e.dataTransfer.getData(ITEM_DRAG_MIME));
        if (!droppedID || droppedID === item.id) return;
        onDropAbove(droppedID);
      }}
      onContextMenu={(e) => ctx.onContextMenu(e)}
      className={
        'flex items-center gap-1 px-2 py-1 text-xs transition-all ' +
        (isDragging ? 'opacity-40 ' : '') +
        (dragOverHere ? 'border-t-2 border-accent ' : '')
      }
    >
      <button
        onClick={() =>
          onUpdate({ text: item.text, is_favorite: !item.is_favorite, position: item.position })
        }
        className={
          'shrink-0 rounded p-1 hover:bg-bg-elevated ' +
          (item.is_favorite ? 'text-warning' : 'text-fg-muted/50 hover:text-fg-muted')
        }
        title={item.is_favorite ? 'Remove favourite' : 'Mark as favourite'}
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
          onMouseDown={(e) => e.stopPropagation()}
          className="flex-1 rounded border border-border bg-bg-elevated px-2 py-0.5 text-xs outline-none focus:border-accent"
        />
      ) : (
        <span
          className="flex-1 cursor-grab truncate select-none"
          title={item.text}
        >
          {item.text || <span className="italic text-fg-muted">empty</span>}
        </span>
      )}
      <ContextMenu open={ctx.open} x={ctx.x} y={ctx.y} items={menuItems} onClose={ctx.close} />
    </li>
  );
}
