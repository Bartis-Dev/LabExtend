// The floating favourites card's visibility/position/size lives in
// localStorage so it stays consistent across page reloads but is
// per-device (the user might want it in different places on a laptop
// vs. a workstation). Not stored on the server.
import { create } from 'zustand';

const STORAGE_KEY = 'labextend.notes.fav-card';

type Persisted = {
  visible: boolean;
  x: number;
  y: number;
  w: number;
  h: number;
};

const DEFAULTS: Persisted = {
  visible: false,
  x: 24,
  y: 24,
  w: 320,
  h: 380,
};

function load(): Persisted {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULTS;
    const parsed = JSON.parse(raw) as Partial<Persisted>;
    return {
      visible: !!parsed.visible,
      x: Number.isFinite(parsed.x) ? Number(parsed.x) : DEFAULTS.x,
      y: Number.isFinite(parsed.y) ? Number(parsed.y) : DEFAULTS.y,
      w: Number.isFinite(parsed.w) ? Number(parsed.w) : DEFAULTS.w,
      h: Number.isFinite(parsed.h) ? Number(parsed.h) : DEFAULTS.h,
    };
  } catch {
    return DEFAULTS;
  }
}

function save(p: Persisted) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(p));
  } catch {
    /* quota, private mode, etc. — best effort */
  }
}

type State = Persisted & {
  toggle: () => void;
  setVisible: (v: boolean) => void;
  setPos: (x: number, y: number) => void;
  setSize: (w: number, h: number) => void;
  clampToViewport: () => void;
};

const MIN_W = 220;
const MIN_H = 160;
const MAX_W = 800;
const MAX_H = 900;
const VISIBLE_MARGIN = 80;

export const useFavCard = create<State>((set, get) => ({
  ...load(),

  toggle() {
    const v = !get().visible;
    set({ visible: v });
    save({ ...get(), visible: v });
  },
  setVisible(v) {
    set({ visible: v });
    save({ ...get(), visible: v });
  },
  setPos(x, y) {
    set({ x, y });
    save({ ...get(), x, y });
  },
  setSize(w, h) {
    const cw = Math.max(MIN_W, Math.min(MAX_W, w));
    const ch = Math.max(MIN_H, Math.min(MAX_H, h));
    set({ w: cw, h: ch });
    save({ ...get(), w: cw, h: ch });
  },
  // Called on viewport resize: nudge the card back inside if it slid out.
  clampToViewport() {
    const { x, y, w } = get();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const newX = Math.max(-w + VISIBLE_MARGIN, Math.min(vw - VISIBLE_MARGIN, x));
    const newY = Math.max(0, Math.min(vh - VISIBLE_MARGIN, y));
    if (newX !== x || newY !== y) {
      set({ x: newX, y: newY });
      save({ ...get(), x: newX, y: newY });
    }
  },
}));
