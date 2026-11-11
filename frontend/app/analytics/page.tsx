'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { api, errText } from '@/lib/api';
import type { WebCount, WebHost, WebReport } from '@/lib/types';
import { AuthShell } from '@/components/auth-shell';
import { MetricChart } from '@/components/metric-chart';
import { BlockSkeleton, ErrorState } from '@/components/feedback';

export default function AnalyticsPage() {
  return (
    <AuthShell>
      <Analytics />
    </AuthShell>
  );
}

type Range = '24h' | '7d' | '30d' | '90d' | '365d';
const RANGES: Range[] = ['24h', '7d', '30d', '90d', '365d'];

const HOST_KEY = 'web-analytics-host';
const RANGE_KEY = 'web-analytics-range';

function readPref(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function writePref(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value);
  } catch {/* private mode: the choice just is not remembered */}
}

const errorText = (e: unknown) => errText(e, 'Could not load analytics. Check that the leader is reachable and try again.');

function Analytics() {
  const [hosts, setHosts] = useState<WebHost[] | null>(null);
  const [host, setHost] = useState('');
  const [range, setRange] = useState<Range>('7d');
  const [report, setReport] = useState<WebReport | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const r = readPref(RANGE_KEY);
    if (r && (RANGES as string[]).includes(r)) setRange(r as Range);
  }, []);

  const loadHosts = useCallback(async () => {
    try {
      const r = await api<{ hosts: WebHost[] }>('/api/analytics/hosts');
      const list = r.hosts ?? [];
      setHosts(list);
      setHost((cur) => {
        if (cur && list.some((h) => h.host === cur)) return cur;
        const saved = readPref(HOST_KEY);
        if (saved && list.some((h) => h.host === saved)) return saved;
        return list[0]?.host ?? '';
      });
    } catch (e) {
      setError(errorText(e));
      setHosts((h) => h ?? []);
    }
  }, []);

  // Only the newest report request may write (or clear the spinner): a slow
  // 365d answer must not land after the user switched to 24h.
  const reportSeq = useRef(0);
  const loadReport = useCallback(async (h: string, r: Range) => {
    if (!h) return;
    const my = ++reportSeq.current;
    setLoading(true);
    try {
      const rep = await api<WebReport>('/api/analytics/report', { query: { host: h, range: r } });
      if (my !== reportSeq.current) return;
      setReport(rep);
      setError(null);
    } catch (e) {
      if (my !== reportSeq.current) return;
      setError(errorText(e));
    } finally {
      if (my === reportSeq.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadHosts();
    const t = setInterval(loadHosts, 60_000);
    return () => clearInterval(t);
  }, [loadHosts]);

  useEffect(() => {
    loadReport(host, range);
    // The leader flushes every 30s; polling faster would only redraw the same numbers.
    const t = setInterval(() => loadReport(host, range), 30_000);
    return () => clearInterval(t);
  }, [host, range, loadReport]);

  const pickHost = (h: string) => {
    setReport(null);
    setHost(h);
    writePref(HOST_KEY, h);
  };
  const pickRange = (r: Range) => {
    setRange(r);
    writePref(RANGE_KEY, r);
  };

  const current = report && report.host === host && report.range === range ? report : null;

  return (
    <div className="page">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="page-title">Web Analytics</h1>
          <p className="page-sub">
            Page views from Traefik access logs. No scripts on the sites, no IP addresses stored. Times in UTC.
          </p>
        </div>
        {hosts && hosts.length > 0 && (
          <div className="flex flex-wrap items-center gap-3">
            <label className="flex items-center gap-2 text-xs font-medium text-zinc-600 dark:text-zinc-400">
              <span>Site</span>
              <select
                className="input w-64"
                value={host}
                onChange={(e) => pickHost(e.target.value)}
              >
                {hosts.map((h) => (
                  <option key={h.host} value={h.host}>{h.host}</option>
                ))}
              </select>
            </label>
            <div className="seg" role="group" aria-label="Time range">
              {RANGES.map((r) => (
                <button
                  key={r}
                  type="button"
                  aria-pressed={r === range}
                  onClick={() => pickRange(r)}
                  className={r === range ? 'seg-item-active' : 'seg-item'}
                >
                  {r}
                </button>
              ))}
            </div>
          </div>
        )}
      </header>

      {error && <ErrorState message={error} onRetry={() => { loadHosts(); loadReport(host, range); }} />}

      {hosts === null ? (
        <BlockSkeleton />
      ) : hosts.length === 0 ? (
        <EmptyState />
      ) : !current ? (
        loading || !error ? <BlockSkeleton /> : null
      ) : (
        <ReportView report={current} />
      )}
    </div>
  );
}

function EmptyState() {
  return (
    <div className="card max-w-3xl">
      <h2 className="mb-1 text-sm font-semibold">No traffic recorded yet</h2>
      <p className="text-sm text-zinc-600 dark:text-zinc-400">
        LabExtend counts page views from the access log of every container whose image starts with{' '}
        <code className="rounded bg-zinc-100 px-1 py-0.5 text-xs dark:bg-zinc-800">traefik</code>{' '}
        (<code className="rounded bg-zinc-100 px-1 py-0.5 text-xs dark:bg-zinc-800">BPM_ANALYTICS_IMAGE_PREFIX</code>).
        Traefik must write its access log as JSON to stdout, with the{' '}
        <code className="rounded bg-zinc-100 px-1 py-0.5 text-xs dark:bg-zinc-800">User-Agent</code> and{' '}
        <code className="rounded bg-zinc-100 px-1 py-0.5 text-xs dark:bg-zinc-800">Referer</code> headers kept.
        Numbers appear here within about 30 seconds of the first request.
      </p>
    </div>
  );
}

const nf = new Intl.NumberFormat();

function ReportView({ report }: { report: WebReport }) {
  const t = report.totals;
  const series = report.series ?? [];
  const multiDay = report.bucket === 'day';
  return (
    <>
      <div className="mb-4 grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Stat label="Page views" value={t.pageviews} />
        <Stat
          label="Visitors"
          value={t.visitors}
          hint={multiDay ? 'Sum of daily unique visitors. Visitors cannot be recognised across days, by design.' : 'Unique visitors per day, counted in the hour they first appeared.'}
        />
        <Stat label="Bot hits" value={t.bot_hits} hint="Requests from crawlers, monitors and tools. Not in page views or visitors." />
        <Stat label="Requests" value={t.requests} hint="Every request Traefik routed for this site, including assets and bots." />
      </div>

      <div className="card mb-4">
        <h3 className="section-title mb-3">
          Page views and visitors {multiDay ? 'per day' : 'per hour'}
        </h3>
        <MetricChart
          timestamps={series.map((p) => p.ts)}
          formatY={(v) => nf.format(Math.round(v))}
          series={[
            { label: 'Page views', color: '#2563EB', values: series.map((p) => p.pageviews) },
            { label: 'Visitors', color: '#10B981', values: series.map((p) => p.visitors) },
          ]}
        />
      </div>

      {report.range === '24h' && (
        <p className="mb-2 text-2xs text-zinc-500">Breakdowns are kept per day, so for 24h they cover today and yesterday (UTC).</p>
      )}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2 3xl:grid-cols-3">
        <Breakdown title="Top pages" rows={report.paths} mono className="lg:col-span-2" />
        <Breakdown title="Referrers" rows={report.referrers} />
        <Breakdown title="Status codes" rows={report.status} empty="No requests in this range." />
        <Breakdown title="Browsers" rows={report.browsers} />
        <Breakdown title="Operating systems" rows={report.os} />
      </div>
    </>
  );
}

function Stat({ label, value, hint }: { label: string; value: number; hint?: string }) {
  return (
    <div className="card">
      <div className="text-2xs font-medium uppercase tracking-wide text-zinc-500">{label}</div>
      <div className="mt-1 text-3xl font-semibold tabular-nums tracking-tight">{nf.format(value)}</div>
      {hint && <p className="hint mt-2 leading-snug">{hint}</p>}
    </div>
  );
}

function Breakdown({
  title, rows, mono, className, empty = 'No page views in this range.',
}: {
  title: string;
  rows: WebCount[] | null | undefined;
  mono?: boolean;
  className?: string;
  empty?: string;
}) {
  const list = rows ?? [];
  const total = list.reduce((s, r) => s + r.count, 0) || 1;
  return (
    <div className={`card overflow-hidden p-0 ${className ?? ''}`}>
      <h3 className="section-title border-b border-zinc-200 px-4 py-3 dark:border-zinc-800">
        {title}
      </h3>
      {list.length === 0 ? (
        <div className="px-4 py-10 text-center text-sm text-zinc-500">{empty}</div>
      ) : (
        <div className="max-h-96 overflow-auto">
          <table className="w-full text-sm">
            <thead className="sr-only">
              <tr><th>Value</th><th>Count</th><th>Share</th></tr>
            </thead>
            <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800/70">
              {list.map((r) => {
                const pct = (100 * r.count) / total;
                return (
                  <tr key={r.value}>
                    <td className="td relative max-w-0 w-full">
                      <span
                        aria-hidden
                        className="absolute inset-y-1 left-0 rounded-r bg-accent/10"
                        style={{ width: `${pct}%` }}
                      />
                      <span className={`relative block truncate ${mono ? 'font-mono text-xs' : ''}`} title={r.value}>
                        {r.value}
                      </span>
                    </td>
                    <td className="td text-right font-mono text-xs tabular-nums">{nf.format(r.count)}</td>
                    <td className="td w-14 text-right text-2xs tabular-nums text-zinc-500">{pct.toFixed(0)}%</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
