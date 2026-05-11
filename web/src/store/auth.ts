import { create } from 'zustand';
import { api, ApiError } from '@/api/client';
import type { Bootstrap, LoginReq, Me, SetupReq } from '@/api/types';

type State = {
  ready: boolean;
  needsSetup: boolean | null;
  user: Me | null;
  bootstrap: () => Promise<Bootstrap | null>;
  login: (req: LoginReq) => Promise<void>;
  setup: (req: SetupReq) => Promise<void>;
  logout: () => Promise<void>;
};

export const useAuth = create<State>((set) => ({
  ready: false,
  needsSetup: null,
  user: null,
  bootstrap: async () => {
    try {
      const b = await api.get<Bootstrap>('/api/bootstrap');
      let me: Me | null = null;
      if (!b.needs_setup) {
        try {
          me = await api.get<Me>('/api/auth/me');
        } catch (err) {
          if (!(err instanceof ApiError) || err.status !== 401) throw err;
        }
      }
      set({ ready: true, needsSetup: b.needs_setup, user: me });
      return b;
    } catch (err) {
      set({ ready: true });
      throw err;
    }
  },
  login: async (req) => {
    const me = await api.post<Me>('/api/auth/login', req);
    set({ user: me });
  },
  setup: async (req) => {
    const me = await api.post<Me>('/api/setup', req);
    set({ user: me, needsSetup: false });
  },
  logout: async () => {
    await api.post('/api/auth/logout');
    set({ user: null });
  },
}));
