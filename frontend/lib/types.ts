export interface MetricsSample {
  node_id: string;
  reported_at: number;
  uptime_seconds: number;
  load_avg_1m: number;
  cpu_percent: number;
  cpu_cores: number;
  mem_used_bytes: number;
  mem_total_bytes: number;
  mem_percent: number;
  disk_used_bytes: number;
  disk_total_bytes: number;
  disk_percent: number;
  net_rx_bytes: number;
  net_tx_bytes: number;
  net_rx_bps: number;
  net_tx_bps: number;
  disk_read_bytes: number;
  disk_write_bytes: number;
  disk_read_bps: number;
  disk_write_bps: number;
}

export interface MetricsBucket {
  bucket_minute: number;
  cpu_percent: number;
  mem_percent: number;
  disk_percent: number;
  net_rx_bps: number;
  net_tx_bps: number;
  disk_read_bps: number;
  disk_write_bps: number;
}

export interface NodeView {
  id: string;
  hostname: string;
  os: string;
  arch: string;
  version: string;
  status: 'online' | 'offline';
  labels: Record<string, string>;
  first_seen: number;
  last_seen: number;
  metrics?: MetricsSample;
}

export interface ContainerView {
  node_id: string;
  container_id: string;
  name: string;
  image: string;
  state: string;
  health: string;
  started_at_ms: number;
  finished_at_ms: number;
  restart_count: number;
  recent_restarts: number;
  crashed_loop: boolean;
  exit_code: number;
  cpu_percent: number;
  mem_used_bytes: number;
  mem_limit_bytes: number;
  mem_percent: number;
  net_rx_bytes: number;
  net_tx_bytes: number;
  net_rx_bps: number;
  net_tx_bps: number;
  block_read_bytes: number;
  block_write_bytes: number;
  labels: Record<string, string>;
  reported_at: number;
}

export interface LogEntry {
  id: number;
  node_id: string;
  container_id: string;
  stream: 'stdout' | 'stderr';
  ts_ms: number;
  line: string;
}

export interface AlertRule {
  id: string;
  name: string;
  kind: string;
  comparator: '>' | '>=' | '<' | '<=';
  threshold: number;
  duration_sec: number;
  scope: string;
  webhook_id?: string | null;
  cooldown_sec: number;
  enabled: boolean;
  created_at: number;
  updated_at: number;
}

export interface AlertHistoryRow {
  id: number;
  rule_id: string;
  rule_name?: string;
  node_id?: string;
  container_id?: string;
  fired_at: number;
  state: 'triggered' | 'recovered';
  value: number;
  message: string;
}

export interface WebhookConfig {
  id: string;
  name: string;
  kind: string;
  url?: string;
  enabled: boolean;
  created_at: number;
  updated_at: number;
}

export interface UserInfo {
  id: number;
  email: string;
  display_name: string;
  is_admin: boolean;
  csrf_token: string;
}
