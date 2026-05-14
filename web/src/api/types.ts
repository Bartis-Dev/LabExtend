// Auth + bootstrap
export type ActiveTheme = {
  id: number;
  name: string;
  palette_json: string;
  custom_css: string;
  is_default: boolean;
};

export type Bootstrap = {
  needs_setup: boolean;
  active_theme: ActiveTheme;
};

export type Me = { username: string };
export type LoginReq = { username: string; password: string };
export type SetupReq = {
  username: string;
  password: string;
  password_confirm: string;
};
export type ChangePasswordReq = {
  current: string;
  new: string;
  new_confirm: string;
};

// Services + categories
export type LayoutRect = { x: number; y: number; w: number; h: number };

export type Service = {
  id: string; // UUID (32-char hex)
  name: string;
  description: string;
  host_primary: string;
  port_primary?: number | null;
  host_alt?: string | null;
  port_alt?: number | null;
  icon_path?: string | null;
  category_id?: number | null;
  layout: LayoutRect;
  ping_primary: boolean;
  ping_alt: boolean;
  hc_primary_enabled: boolean;
  hc_primary_url?: string | null;
  hc_alt_enabled: boolean;
  hc_alt_url?: string | null;
};

// icon_path is included so it can be set on POST (e.g. when picking an
// icon during service creation). PUT ignores this field server-side —
// icon changes on existing services go through the dedicated endpoints
// (POST /icon for files, PUT /icon-url for URLs, DELETE /icon to clear).
export type ServiceInput = Omit<Service, 'id'>;

export type Category = {
  id: number;
  name: string;
  border_color: string;
  layout: LayoutRect;
};

export type CategoryInput = Omit<Category, 'id'>;

// Themes
export type Palette = Record<string, string>;
export type Theme = {
  id: number;
  name: string;
  palette: Palette;
  custom_css: string;
  is_default: boolean;
  is_active: boolean;
};
export type ThemeInput = {
  name: string;
  palette: Palette;
  custom_css: string;
};

// Healthcheck
export type HostStatus = 'up' | 'down' | 'n/a';
export type ServiceStatus = { primary: HostStatus; alt: HostStatus };
// Keyed by service UUID (string). Backend currently sends numeric internal IDs
// via WS; the worker is updated separately to emit UUID keys.
export type HealthMap = Record<string, ServiceStatus>;

// Settings
export type Settings = Record<string, string>;

// TLS
export type TLSState = {
  loaded: boolean;
  source?: 'env' | 'data_dir' | 'self_signed';
  subject?: string;
  issuer?: string;
  dns_names?: string[];
  ips?: string[];
  not_before?: string;
  not_after?: string;
  self_signed?: boolean;
  listen: string;
};

// Stats
export type StatsSource = {
  id: number;
  name: string;
  unit: string;
  token: string;
  created_at: number;
  updated_at: number;
};
export type StatsSourceInput = { name: string; unit: string };

export type StatsPoint = { ts: number; value: number };

export type StatsPointsResponse = {
  from: number;
  to: number;
  points: StatsPoint[];
  latest: StatsPoint | null;
};

export type StatsWidgetKind = 'line' | 'gauge';
export type StatsWidget = {
  id: number;
  source_id: number;
  name: string;
  kind: StatsWidgetKind;
  time_range_minutes: number;
  position: number;
  config_json: string;
  created_at: number;
  updated_at: number;
};
export type StatsWidgetInput = {
  source_id: number;
  name: string;
  kind: StatsWidgetKind;
  time_range_minutes: number;
  position: number;
  config_json: string;
};

// Notes
export type NotesItem = {
  id: number;
  card_id: number;
  text: string;
  is_favorite: boolean;
  position: number;
  created_at: number;
  updated_at: number;
};
export type NotesCard = {
  id: number;
  name: string;
  x: number;
  y: number;
  w: number;
  h: number;
  color: string;
  title_color: string;
  board_id: number | null;
  slot_index: number;
  created_at: number;
  updated_at: number;
  items: NotesItem[];
};
export type NotesBoard = {
  id: number;
  name: string;
  x: number;
  y: number;
  cols: number;
  color: string;
  title_color: string;
  created_at: number;
  updated_at: number;
};
export type NotesCardInput = {
  name: string;
  x: number;
  y: number;
  w: number;
  h: number;
  color: string;
  title_color?: string;
  board_id?: number | null;
  slot_index?: number;
};
export type NotesItemInput = {
  text: string;
  is_favorite: boolean;
  position: number;
};
export type NotesBoardInput = {
  name: string;
  x: number;
  y: number;
  cols: number;
  color: string;
  title_color?: string;
};
export type NotesState = {
  cards: NotesCard[];
  boards: NotesBoard[];
};

// Docs
export type DocPage = {
  id: number;
  slug: string;
  title: string;
  category: string;
  content_markdown: string;
  is_link: boolean;
  link_url: string | null;
  position: number;
  created_at: number;
  updated_at: number;
};
export type DocPageInput = {
  slug: string;
  title: string;
  category: string;
  content_markdown: string;
  is_link: boolean;
  link_url: string | null;
  position: number;
};

// Wake on LAN
export type WoLTarget = {
  id: number;
  name: string;
  mac: string;
  broadcast_addr: string;
  port: number;
  ping_host: string;
  ping_port: number;
  last_sent_at: number | null;
  last_error: string | null;
  created_at: number;
  updated_at: number;
};
export type WoLTargetInput = {
  name: string;
  mac: string;
  broadcast_addr: string;
  port: number;
  ping_host: string;
  ping_port: number;
};
export type WoLStatusMap = Record<string, 'up' | 'down'>;

// DDNS
export type DDNSProvider = {
  id: number;
  name: string;
  kind: 'cloudflare';
  created_at: number;
  updated_at: number;
};
export type DDNSProviderInput = { name: string; kind: 'cloudflare'; token: string };

export type DDNSZone = { id: string; name: string };

export type DDNSRecord = {
  id?: string;
  name: string;
  type: string;
  content: string;
  ttl: number;
  proxied: boolean;
  comment?: string;
};

export type DDNSCard = {
  id: number;
  provider_id: number;
  remote_id: string;
  name: string;
  show_types: string[];
  layout: LayoutRect;
  created_at: number;
  updated_at: number;
};

export type DDNSCardInput = {
  provider_id: number;
  remote_id: string;
  name: string;
  show_types: string[];
};

export type DDNSAutoUpdate = {
  id: number;
  card_id: number;
  record_remote_id: string;
  record_name: string;
  record_type: 'A' | 'AAAA';
  last_synced_ip: string | null;
  last_synced_at: number | null;
  last_error: string | null;
  created_at: number;
};

// Modules
export type ModuleKind = 'builtin' | 'iframe';
export type Module = {
  id: number;
  slug: string;
  kind: ModuleKind;
  name: string;
  icon: string;
  url?: string | null;
  enabled: boolean;
  position: number;
  builtin_key?: string | null;
};
export type IframeModuleInput = {
  name: string;
  url: string;
  icon: string;
  slug?: string;
};
export type ModulePatch = Partial<{
  name: string;
  icon: string;
  url: string;
  enabled: boolean;
  position: number;
}>;
