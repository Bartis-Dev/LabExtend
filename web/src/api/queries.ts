import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import type {
  Category,
  CategoryInput,
  DocPage,
  DocPageInput,
  NotesCard,
  NotesCardInput,
  NotesItem,
  NotesItemInput,
  StatsPointsResponse,
  StatsSource,
  StatsSourceInput,
  StatsWidget,
  StatsWidgetInput,
  TLSState,
  DDNSAutoUpdate,
  DDNSCard,
  DDNSCardInput,
  DDNSProvider,
  DDNSProviderInput,
  DDNSRecord,
  DDNSZone,
  HealthMap,
  IframeModuleInput,
  Module,
  ModulePatch,
  Service,
  ServiceInput,
  Settings,
  Theme,
  ThemeInput,
  WoLTarget,
  WoLTargetInput,
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

// --- Modules -------------------------------------------------------------

export const modulesKey = ['modules'] as const;

export function useModules() {
  return useQuery({
    queryKey: modulesKey,
    queryFn: () => api.get<Module[]>('/api/modules'),
  });
}

export function useCreateIframeModule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: IframeModuleInput) =>
      api.post<Module>('/api/modules', { kind: 'iframe', ...input }),
    onSuccess: () => qc.invalidateQueries({ queryKey: modulesKey }),
  });
}

export function useUpdateModule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; patch: ModulePatch }) =>
      api.put<Module>(`/api/modules/${args.id}`, args.patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: modulesKey }),
  });
}

export function useDeleteModule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/modules/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: modulesKey }),
  });
}

// --- Vault ----------------------------------------------------------------

export type VaultEntryRow = {
  id: number;
  payload_ciphertext: string; // base64
  payload_nonce: string; // base64
  created_at: number;
  updated_at: number;
};

export const vaultEntriesKey = ['vault', 'entries'] as const;

export function useVaultEntries(enabled: boolean) {
  return useQuery({
    queryKey: vaultEntriesKey,
    queryFn: () => api.get<VaultEntryRow[]>('/api/vault/entries'),
    enabled,
  });
}

export function useCreateVaultEntry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { payload_ciphertext: string; payload_nonce: string }) =>
      api.post<VaultEntryRow>('/api/vault/entries', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: vaultEntriesKey }),
  });
}

export function useUpdateVaultEntry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; input: { payload_ciphertext: string; payload_nonce: string } }) =>
      api.put<VaultEntryRow>(`/api/vault/entries/${args.id}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: vaultEntriesKey }),
  });
}

export function useDeleteVaultEntry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/vault/entries/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: vaultEntriesKey }),
  });
}

// --- DDNS -----------------------------------------------------------------

export const ddnsProvidersKey = ['ddns', 'providers'] as const;
export const ddnsCardsKey = ['ddns', 'cards'] as const;
export const ddnsAutoUpdatesKey = ['ddns', 'auto-update'] as const;

export function useDDNSProviders() {
  return useQuery({
    queryKey: ddnsProvidersKey,
    queryFn: () => api.get<DDNSProvider[]>('/api/ddns/providers'),
  });
}

export function useCreateDDNSProvider() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DDNSProviderInput) => api.post<DDNSProvider>('/api/ddns/providers', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ddnsProvidersKey }),
  });
}

export function useUpdateDDNSProvider() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; name: string; token?: string }) =>
      api.put<DDNSProvider>(`/api/ddns/providers/${args.id}`, {
        name: args.name,
        token: args.token ?? null,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ddnsProvidersKey }),
  });
}

export function useDeleteDDNSProvider() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/ddns/providers/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ddnsProvidersKey });
      qc.invalidateQueries({ queryKey: ddnsCardsKey });
    },
  });
}

export function useDDNSZones(providerId: number | null) {
  return useQuery({
    queryKey: ['ddns', 'zones', providerId] as const,
    queryFn: () => api.get<DDNSZone[]>(`/api/ddns/providers/${providerId}/zones`),
    enabled: providerId != null,
  });
}

export function useDDNSCards() {
  return useQuery({
    queryKey: ddnsCardsKey,
    queryFn: () => api.get<DDNSCard[]>('/api/ddns/cards'),
  });
}

export function useCreateDDNSCard() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DDNSCardInput) => api.post<DDNSCard>('/api/ddns/cards', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ddnsCardsKey }),
  });
}

export function useUpdateDDNSCard() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; input: { name: string; show_types: string[]; layout: { x: number; y: number; w: number; h: number } } }) =>
      api.put<DDNSCard>(`/api/ddns/cards/${args.id}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ddnsCardsKey }),
  });
}

export function useDeleteDDNSCard() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/ddns/cards/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ddnsCardsKey }),
  });
}

export function useCardRecords(cardId: number | null) {
  return useQuery({
    queryKey: ['ddns', 'records', cardId] as const,
    queryFn: () => api.get<DDNSRecord[]>(`/api/ddns/cards/${cardId}/records`),
    enabled: cardId != null,
  });
}

export function useCreateCardRecord(cardId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DDNSRecord) =>
      api.post<DDNSRecord>(`/api/ddns/cards/${cardId}/records`, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['ddns', 'records', cardId] }),
  });
}

export function useUpdateCardRecord(cardId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { recordId: string; input: DDNSRecord }) =>
      api.put<DDNSRecord>(`/api/ddns/cards/${cardId}/records/${args.recordId}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['ddns', 'records', cardId] }),
  });
}

export function useDeleteCardRecord(cardId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (recordId: string) =>
      api.delete<void>(`/api/ddns/cards/${cardId}/records/${recordId}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['ddns', 'records', cardId] });
      qc.invalidateQueries({ queryKey: ddnsAutoUpdatesKey });
    },
  });
}

export function useToggleAutoUpdate(cardId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      record_remote_id: string;
      record_name: string;
      record_type: 'A' | 'AAAA';
      enabled: boolean;
    }) => api.post<void>(`/api/ddns/cards/${cardId}/auto-update`, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ddnsAutoUpdatesKey }),
  });
}

export function useAutoUpdates() {
  return useQuery({
    queryKey: ddnsAutoUpdatesKey,
    queryFn: () => api.get<DDNSAutoUpdate[]>('/api/ddns/auto-update'),
    refetchInterval: 30_000,
  });
}

// --- Wake on LAN ----------------------------------------------------------

export const wolKey = ['wol'] as const;

export function useWoLTargets() {
  return useQuery({
    queryKey: wolKey,
    queryFn: () => api.get<WoLTarget[]>('/api/wol'),
  });
}

export function useCreateWoLTarget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: WoLTargetInput) => api.post<WoLTarget>('/api/wol', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: wolKey }),
  });
}

export function useUpdateWoLTarget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; input: WoLTargetInput }) =>
      api.put<WoLTarget>(`/api/wol/${args.id}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: wolKey }),
  });
}

export function useDeleteWoLTarget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/wol/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: wolKey }),
  });
}

export function useWakeWoLTarget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.post<void>(`/api/wol/${id}/wake`),
    onSuccess: () => qc.invalidateQueries({ queryKey: wolKey }),
  });
}

// --- Docs -----------------------------------------------------------------

export const docsKey = ['docs'] as const;

export function useDocs() {
  return useQuery({
    queryKey: docsKey,
    queryFn: () => api.get<DocPage[]>('/api/docs'),
  });
}

export function useCreateDoc() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DocPageInput) => api.post<DocPage>('/api/docs', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: docsKey }),
  });
}

export function useUpdateDoc() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; input: DocPageInput }) =>
      api.put<DocPage>(`/api/docs/${args.id}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: docsKey }),
  });
}

export function useDeleteDoc() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/docs/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: docsKey }),
  });
}

// --- Notes ----------------------------------------------------------------

export const notesKey = ['notes'] as const;

export function useNotes(enabled: boolean = true) {
  return useQuery({
    queryKey: notesKey,
    queryFn: () => api.get<NotesCard[]>('/api/notes'),
    enabled,
  });
}

export function useCreateNotesCard() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: NotesCardInput) => api.post<NotesCard>('/api/notes/cards', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: notesKey }),
  });
}

export function useUpdateNotesCard() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; input: NotesCardInput }) =>
      api.put<NotesCard>(`/api/notes/cards/${args.id}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: notesKey }),
  });
}

// Lightweight layout patch — used while dragging cards. Caller is
// responsible for optimistic updates; we don't invalidate here to avoid
// snapping the card back to server state mid-drag.
export function usePatchNotesCardLayout() {
  return useMutation({
    mutationFn: (args: { id: number; x: number; y: number; w: number; h: number }) =>
      api.patch<void>(`/api/notes/cards/${args.id}/layout`, {
        x: args.x,
        y: args.y,
        w: args.w,
        h: args.h,
      }),
  });
}

export function useDeleteNotesCard() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/notes/cards/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: notesKey }),
  });
}

export function useCreateNotesItem() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { cardId: number; input: NotesItemInput }) =>
      api.post<NotesItem>(`/api/notes/cards/${args.cardId}/items`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: notesKey }),
  });
}

export function useUpdateNotesItem() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; input: NotesItemInput }) =>
      api.put<NotesItem>(`/api/notes/items/${args.id}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: notesKey }),
  });
}

export function useDeleteNotesItem() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/notes/items/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: notesKey }),
  });
}

// --- Stats ----------------------------------------------------------------

export const statsSourcesKey = ['stats', 'sources'] as const;
export const statsWidgetsKey = ['stats', 'widgets'] as const;

export function useStatsSources() {
  return useQuery({
    queryKey: statsSourcesKey,
    queryFn: () => api.get<StatsSource[]>('/api/stats/sources'),
  });
}
export function useCreateStatsSource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: StatsSourceInput) => api.post<StatsSource>('/api/stats/sources', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: statsSourcesKey }),
  });
}
export function useUpdateStatsSource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; input: StatsSourceInput }) =>
      api.put<StatsSource>(`/api/stats/sources/${args.id}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: statsSourcesKey }),
  });
}
export function useRotateStatsSourceToken() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.post<StatsSource>(`/api/stats/sources/${id}/rotate-token`),
    onSuccess: () => qc.invalidateQueries({ queryKey: statsSourcesKey }),
  });
}
export function useDeleteStatsSource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/stats/sources/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: statsSourcesKey });
      qc.invalidateQueries({ queryKey: statsWidgetsKey });
    },
  });
}

// Polled query for a widget's time series. The interval is conservative
// so widgets refresh roughly every 15s without hammering SQLite.
export function useStatsPoints(sourceId: number, fromSec: number, toSec: number, refetchMs = 15000) {
  return useQuery({
    queryKey: ['stats', 'points', sourceId, fromSec, toSec] as const,
    queryFn: () =>
      api.get<StatsPointsResponse>(
        `/api/stats/sources/${sourceId}/points?from=${fromSec}&to=${toSec}&max=500`,
      ),
    refetchInterval: refetchMs,
  });
}

export function useStatsWidgets() {
  return useQuery({
    queryKey: statsWidgetsKey,
    queryFn: () => api.get<StatsWidget[]>('/api/stats/widgets'),
  });
}
export function useCreateStatsWidget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: StatsWidgetInput) => api.post<StatsWidget>('/api/stats/widgets', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: statsWidgetsKey }),
  });
}
export function useUpdateStatsWidget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; input: StatsWidgetInput }) =>
      api.put<StatsWidget>(`/api/stats/widgets/${args.id}`, args.input),
    onSuccess: () => qc.invalidateQueries({ queryKey: statsWidgetsKey }),
  });
}
export function useDeleteStatsWidget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.delete<void>(`/api/stats/widgets/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: statsWidgetsKey }),
  });
}

// --- TLS ------------------------------------------------------------------

export const tlsStateKey = ['tls', 'state'] as const;

export function useTLSState() {
  return useQuery({
    queryKey: tlsStateKey,
    queryFn: () => api.get<TLSState>('/api/tls/state'),
  });
}

export function useUploadTLSCert() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { cert_pem: string; key_pem: string }) =>
      api.post<TLSState>('/api/tls/cert', args),
    onSuccess: () => qc.invalidateQueries({ queryKey: tlsStateKey }),
  });
}

export function useGenerateSelfSignedTLS() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { hostnames: string[]; validity_days: number }) =>
      api.post<TLSState>('/api/tls/self-signed', args),
    onSuccess: () => qc.invalidateQueries({ queryKey: tlsStateKey }),
  });
}

export function useResetTLSCert() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<TLSState>('/api/tls/reset'),
    onSuccess: () => qc.invalidateQueries({ queryKey: tlsStateKey }),
  });
}
