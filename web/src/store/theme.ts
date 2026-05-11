import { create } from 'zustand';
import type { ActiveTheme, Palette } from '@/api/types';

type State = {
  active: { id: number; name: string; palette: Palette; customCss: string; isDefault: boolean } | null;
  setFromBootstrap: (t: ActiveTheme) => void;
  setActive: (id: number, name: string, palette: Palette, customCss: string, isDefault: boolean) => void;
};

export const useTheme = create<State>((set) => ({
  active: null,
  setFromBootstrap: (t) => {
    let palette: Palette = {};
    try {
      palette = JSON.parse(t.palette_json) as Palette;
    } catch {
      /* malformed palette: leave empty so :root fallback applies */
    }
    set({
      active: {
        id: t.id,
        name: t.name,
        palette,
        customCss: t.custom_css,
        isDefault: t.is_default,
      },
    });
  },
  setActive: (id, name, palette, customCss, isDefault) =>
    set({ active: { id, name, palette, customCss, isDefault } }),
}));
