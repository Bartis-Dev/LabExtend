import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import type {
  Category,
  CategoryInput,
  HealthMap,
  Service,
  ServiceInput,
  Settings,
  Theme,
  ThemeInput,
} from './types';

// --- Services -------------------------------------------------------------

export const servicesKey = ['services'] as const;

export function useServices() {
  return useQuery({ queryKey: servicesKey, queryFn: () => api.get<Service[]>('/api/services') });
}

export function useCreateService() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ServiceInput) => api.post<Service>('/api/services', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: servicesKey }),
  });
}

export function useUpdateService() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: string; input: ServiceInput }) =>
      api.put<Service>(`/api/services/${args.id}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: servicesKey }),
  });
}

export function useDeleteService() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete<void>(`/api/services/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: servicesKey }),
  });
}

export function useSetIconURL() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: string; url: string }) =>
      api.put<{ icon_path: string }>(`/api/services/${args.id}/icon-url`, {
        url: args.url,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: servicesKey }),
  });
}

export function useDeleteIcon() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete<void>(`/api/services/${id}/icon`),
    onSuccess: () => qc.invalidateQueries({ queryKey: servicesKey }),
  });
}

// --- Categories -----------------------------------------------------------

export const categoriesKey = ['categories'] as const;

export function useCategories() {
  return useQuery({ queryKey: categoriesKey, queryFn: () => api.get<Category[]>('/api/categories') });
}

export function useCreateCategory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CategoryInput) => api.post<Category>('/api/categories', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: categoriesKey }),
  });
}

export function useUpdateCategory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; input: CategoryInput }) =>
      api.put<Category>(`/api/categories/${args.id}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: categoriesKey }),
  });
}

export function useDeleteCategory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/categories/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: categoriesKey });
      qc.invalidateQueries({ queryKey: servicesKey });
    },
  });
}

// --- Bulk layout ----------------------------------------------------------

export type LayoutPayload = {
  services: { id: string; x: number; y: number; w: number; h: number; category_id?: number | null }[];
  categories: { id: number; x: number; y: number; w: number; h: number }[];
};

export function useUpdateLayout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: LayoutPayload) => api.put<void>('/api/layout', payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: servicesKey });
      qc.invalidateQueries({ queryKey: categoriesKey });
    },
  });
}

// --- Themes ---------------------------------------------------------------

export const themesKey = ['themes'] as const;

export function useThemes() {
  return useQuery({ queryKey: themesKey, queryFn: () => api.get<Theme[]>('/api/themes') });
}

export function useCreateTheme() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ThemeInput) => api.post<Theme>('/api/themes', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: themesKey }),
  });
}

export function useUpdateTheme() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; input: ThemeInput }) =>
      api.put<Theme>(`/api/themes/${args.id}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: themesKey }),
  });
}

export function useDeleteTheme() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/themes/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: themesKey }),
  });
}

export function useActivateTheme() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.post<Theme>(`/api/themes/${id}/activate`),
    onSuccess: () => qc.invalidateQueries({ queryKey: themesKey }),
  });
}

// --- Healthcheck status --------------------------------------------------

export const healthKey = ['healthcheck'] as const;

// parseDurationMs handles the same 'Nd / Nh / Nm / Ns' suffixes the backend
// uses, returning milliseconds for setInterval / TanStack's refetchInterval.
function parseDurationMs(s: string | undefined, fallback: number): number {
  if (!s) return fallback;
  const m = s.match(/^(\d+)\s*(ms|s|m|h)?$/);
  if (!m) return fallback;
  const n = Number(m[1]);
  switch (m[2]) {
    case 'ms':
      return n;
    case 'm':
      return n * 60_000;
    case 'h':
      return n * 3_600_000;
    case 's':
    default:
      return n * 1000;
  }
}

export function useHealth() {
  const settings = useSettings();
  const ms = parseDurationMs(settings.data?.status_refresh_interval, 10_000);
  return useQuery({
    queryKey: healthKey,
    queryFn: () => api.get<HealthMap>('/api/healthcheck/status'),
    refetchInterval: ms,
  });
}

// --- Settings ------------------------------------------------------------

export const settingsKey = ['settings'] as const;

export function useSettings() {
  return useQuery({ queryKey: settingsKey, queryFn: () => api.get<Settings>('/api/settings') });
}

export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: Settings) => api.put<Settings>('/api/settings', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: settingsKey }),
  });
}
