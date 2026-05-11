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
  id: number;
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

export type ServiceInput = Omit<Service, 'id' | 'icon_path'>;

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
export type HealthMap = Record<number, ServiceStatus>;

// Settings
export type Settings = Record<string, string>;
