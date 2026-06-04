export function fmtBytes(n: number | undefined | null, decimals = 1): string {
  if (n === undefined || n === null || !Number.isFinite(n) || n <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(decimals)} ${units[i]}`;
}

export function fmtBps(n: number | undefined | null): string {
  return fmtBytes(n ?? 0) + '/s';
}

export function fmtPct(n: number | undefined | null, decimals = 0): string {
  if (n === undefined || n === null || !Number.isFinite(n)) return '0%';
  return `${n.toFixed(decimals)}%`;
}

export function fmtUptime(seconds: number | undefined | null): string {
  if (!seconds || seconds <= 0) return '—';
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

export function fmtRelative(unixSecOrMs: number | undefined | null): string {
  if (!unixSecOrMs) return '—';
  // Detect ms vs s.
  const ms = unixSecOrMs > 1e12 ? unixSecOrMs : unixSecOrMs * 1000;
  const diff = Date.now() - ms;
  if (diff < 0) return 'in the future';
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return `${Math.floor(diff / 86_400_000)}d ago`;
}

export function fmtAbsTime(unixSecOrMs: number | undefined | null): string {
  if (!unixSecOrMs) return '—';
  const ms = unixSecOrMs > 1e12 ? unixSecOrMs : unixSecOrMs * 1000;
  return new Date(ms).toLocaleString();
}
