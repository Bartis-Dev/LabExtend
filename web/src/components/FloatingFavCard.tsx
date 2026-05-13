import { useEffect, useMemo, useRef } from 'react';
import { createPortal } from 'react-dom';
import { useNotes, useUpdateNotesItem } from '@/api/queries';
import { useFavCard } from '@/store/favCard';
import { useAuth } from '@/store/auth';
import { useModules } from '@/api/queries';
import { ModuleIcon } from './ModuleIcon';

export function FloatingFavCardHost() {
  const ready = useAuth((s) => s.ready);
  const user = useAuth((s) => s.user);
  const modules = useModules();
  const visible = useFavCard((s) => s.visible);

  // Only render once auth is ready, user is logged in, the Notes module
  // is enabled, and the toggle is on.
  const notesEnabled = (modules.data ?? []).find((m) => m.slug === 'notes')?.enabled === true;
  if (!ready || !user || !notesEnabled || !visible) return null;
  return <FloatingFavCard />;
}

function FloatingFavCard() {
  const { x, y, w, h, setPos, setSize, setVisible, clampToViewport } = useFavCard();
  const notes = useNotes(true);
  const updateItem = useUpdateNotesItem();

  // ---- Drag the title bar ------------------------------------------------
  const dragState = useRef<{ sx: number; sy: number; x0: number; y0: number } | null>(null);
  const onHeaderMouseDown = (e: React.MouseEvent) => {
    if (e.button !== 0) return;
    if ((e.target as HTMLElement).closest('button')) return;
    e.preventDefault();
    dragState.current = { sx: e.clientX, sy: e.clientY, x0: x, y0: y };
  };
  useEffect(() => {
    const move = (e: MouseEvent) => {
      if (!dragState.current) return;
      setPos(
        dragState.current.x0 + (e.clientX - dragState.current.sx),
        dragState.current.y0 + (e.clientY - dragState.current.sy),
      );
    };
    const up = () => {
      if (dragState.current) {
        dragState.current = null;
        clampToViewport();
      }
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
  }, [setPos, clampToViewport]);

  // ---- Resize the bottom-right corner ------------------------------------
  const resizeState = useRef<{ sx: number; sy: number; w0: number; h0: number } | null>(null);
  const onResizeMouseDown = (e: React.MouseEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    resizeState.current = { sx: e.clientX, sy: e.clientY, w0: w, h0: h };
  };
  useEffect(() => {
    const move = (e: MouseEvent) => {
      if (!resizeState.current) return;
      setSize(
        resizeState.current.w0 + (e.clientX - resizeState.current.sx),
        resizeState.current.h0 + (e.clientY - resizeState.current.sy),
      );
    };
    const up = () => {
      resizeState.current = null;
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
  }, [setSize]);

  // ---- Keep inside viewport on window resize -----------------------------
  useEffect(() => {
    const onResize = () => clampToViewport();
    window.addEventListener('resize', onResize);
    // Also clamp once on mount in case window shrunk while toggle was off.
    clampToViewport();
    return () => window.removeEventListener('resize', onResize);
  }, [clampToViewport]);

  // ---- Build the favourite list -----------------------------------------
  type FlatFav = {
    itemId: number;
    cardId: number;
    cardName: string;
    cardColor: string;
    text: string;
    position: number;
  };
  const favs = useMemo<FlatFav[]>(() => {
    const out: FlatFav[] = [];
    for (const c of notes.data?.cards ?? []) {
      for (const it of c.items) {
        if (it.is_favorite) {
          out.push({
            itemId: it.id,
            cardId: c.id,
            cardName: c.name || 'untitled',
            cardColor: c.color,
            text: it.text,
            position: it.position,
          });
        }
      }
    }
    return out;
  }, [notes.data?.cards]);

  const node = (
    <div
      role="dialog"
      aria-label="Favourite notes"
      className="fixed z-[100] flex flex-col rounded-lg border border-border bg-bg-card shadow-2xl"
      style={{ left: x, top: y, width: w, height: h }}
    >
      <div
        onMouseDown={onHeaderMouseDown}
        className="flex shrink-0 cursor-grab items-center gap-2 rounded-t-lg border-b border-border bg-bg-elevated px-3 py-2 active:cursor-grabbing"
      >
        <ModuleIcon name="star" className="h-4 w-4 text-warning" />
        <span className="flex-1 text-sm font-semibold">Favourites</span>
        <span className="text-xs text-fg-muted">{favs.length}</span>
        <button
          onClick={() => setVisible(false)}
          className="rounded p-1 text-fg-muted hover:bg-bg-card hover:text-fg"
          aria-label="Hide favourites"
          title="Hide"
        >
          ✕
        </button>
      </div>

      <div className="flex-1 overflow-y-auto p-2">
        {notes.isPending && <div className="px-2 py-1 text-xs text-fg-muted">Loading…</div>}
        {!notes.isPending && favs.length === 0 && (
          <div className="px-2 py-6 text-center text-xs text-fg-muted">
            No favourites yet. Star a note in any card to pin it here.
          </div>
        )}
        {favs.length > 0 && (
          <ul className="space-y-1">
            {favs.map((f) => (
              <li
                key={f.itemId}
                className="flex items-start gap-2 rounded border border-border bg-bg-elevated px-2 py-1.5 text-xs"
              >
                <span
                  className="mt-1 h-2 w-2 shrink-0 rounded-full"
                  style={{ background: f.cardColor }}
                  aria-hidden
                />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[10px] uppercase tracking-wider text-fg-muted/80">
                    {f.cardName}
                  </div>
                  <div className="break-words text-fg">{f.text || '(empty)'}</div>
                </div>
                <button
                  onClick={() =>
                    updateItem.mutate({
                      id: f.itemId,
                      input: { text: f.text, is_favorite: false, position: f.position },
                    })
                  }
                  className="shrink-0 rounded p-0.5 text-warning hover:bg-bg-card"
                  title="Unstar"
                  aria-label="Unstar"
                >
                  ★
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div
        onMouseDown={onResizeMouseDown}
        className="absolute bottom-0 right-0 h-4 w-4 cursor-nwse-resize"
        style={{
          background:
            'linear-gradient(135deg, transparent 0 60%, var(--fg-muted) 60% 65%, transparent 65% 75%, var(--fg-muted) 75% 80%, transparent 80%)',
          opacity: 0.6,
        }}
        aria-label="Resize"
      />
    </div>
  );

  return createPortal(node, document.body);
}
