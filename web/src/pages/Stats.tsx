import { useMemo, useState } from 'react';
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { ApiError } from '@/api/client';
import {
  useCreateStatsSource,
  useCreateStatsWidget,
  useDeleteStatsSource,
  useDeleteStatsWidget,
  useRotateStatsSourceToken,
  useStatsPoints,
  useStatsSources,
  useStatsWidgets,
  useUpdateStatsWidget,
} from '@/api/queries';
import type { StatsSource, StatsWidget } from '@/api/types';
import { ModuleIcon } from '@/components/ModuleIcon';

const TIME_RANGES: { minutes: number; label: string }[] = [
  { minutes: 15, label: '15m' },
  { minutes: 60, label: '1h' },
  { minutes: 6 * 60, label: '6h' },
  { minutes: 24 * 60, label: '24h' },
  { minutes: 7 * 24 * 60, label: '7d' },
  { minutes: 30 * 24 * 60, label: '30d' },
];

export default function Stats() {
  const sources = useStatsSources();
  const widgets = useStatsWidgets();
  const [sourcesOpen, setSourcesOpen] = useState(false);
  const [addingWidget, setAddingWidget] = useState(false);
  const [editing, setEditing] = useState<StatsWidget | null>(null);

  const sourcesData = sources.data ?? [];
  const widgetsData = widgets.data ?? [];

  return (
    <div className="mx-auto max-w-7xl p-6">
      <header className="mb-6 flex items-center gap-3">
        <h1 className="flex-1 text-2xl font-bold">Stats</h1>
        <button
          onClick={() => setSourcesOpen(true)}
          className="rounded border border-border px-3 py-2 text-sm hover:bg-bg-elevated"
        >
          Sources ({sourcesData.length})
        </button>
        <button
          onClick={() => setAddingWidget(true)}
          disabled={sourcesData.length === 0}
          className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
          title={sourcesData.length === 0 ? 'Add a source first' : ''}
        >
          + Add widget
        </button>
      </header>

      {sourcesData.length === 0 && (
        <div className="mb-6 rounded-lg border border-warning/40 bg-warning/10 p-4 text-sm">
          You haven&apos;t defined any metric sources yet. Click
          &ldquo;Sources&rdquo; to create one — then push values to its ingest
          endpoint from your scripts.
        </div>
      )}

      {widgets.isPending ? (
        <div className="grid h-40 place-items-center text-fg-muted">Loading…</div>
      ) : widgetsData.length === 0 ? (
        <div className="grid h-48 place-items-center rounded-lg border border-dashed border-border bg-bg-card/30 text-center text-fg-muted">
          <div>
            <p>No widgets yet.</p>
            <p className="mt-1 text-sm">Click &ldquo;+ Add widget&rdquo; to chart a metric.</p>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {widgetsData.map((w) => {
            const src = sourcesData.find((s) => s.id === w.source_id);
            if (!src) return null;
            return <WidgetCard key={w.id} widget={w} source={src} onEdit={() => setEditing(w)} />;
          })}
        </div>
      )}

      {sourcesOpen && <SourcesModal onClose={() => setSourcesOpen(false)} />}
      {addingWidget && (
        <WidgetModal
          mode="create"
          sources={sourcesData}
          onClose={() => setAddingWidget(false)}
        />
      )}
      {editing && (
        <WidgetModal
          mode="edit"
          widget={editing}
          sources={sourcesData}
          onClose={() => setEditing(null)}
        />
      )}
    </div>
  );
}

// ---- WidgetCard ---------------------------------------------------------

function WidgetCard({
  widget,
  source,
  onEdit,
}: {
  widget: StatsWidget;
  source: StatsSource;
  onEdit: () => void;
}) {
  // Recompute the window each render so polling fetches a moving range.
  const now = Math.floor(Date.now() / 1000);
  const from = now - widget.time_range_minutes * 60;
  const points = useStatsPoints(widget.source_id, from, now);

  const data = useMemo(
    () =>
      (points.data?.points ?? []).map((p) => ({
        ts: p.ts * 1000, // recharts expects ms
        value: p.value,
      })),
    [points.data],
  );

  const latest = points.data?.latest;

  return (
    <div className="flex flex-col rounded-lg border border-border bg-bg-card/40">
      <div className="flex items-center gap-2 border-b border-border px-4 py-2">
        <ModuleIcon name="bar-chart-3" className="h-4 w-4 text-fg-muted" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{widget.name}</div>
          <div className="truncate text-xs text-fg-muted">
            {source.name}
            {source.unit && <span className="ml-1">· {source.unit}</span>}
            <span className="ml-2 text-fg-muted/60">last {formatRange(widget.time_range_minutes)}</span>
          </div>
        </div>
        {latest && (
          <div className="text-right">
            <div className="font-mono text-lg">{formatValue(latest.value)}</div>
            <div className="text-[10px] text-fg-muted">{timeAgo(latest.ts)}</div>
          </div>
        )}
        <button
          onClick={onEdit}
          className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-elevated"
        >
          Edit
        </button>
      </div>

      <div className="h-48 px-2 pb-2 pt-3">
        {widget.kind === 'gauge' ? (
          <Gauge value={latest?.value ?? null} unit={source.unit} />
        ) : data.length === 0 ? (
          <div className="grid h-full place-items-center text-xs text-fg-muted">
            No data points in the selected range.
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={data} margin={{ left: 0, right: 8, top: 8, bottom: 0 }}>
              <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" vertical={false} />
              <XAxis
                dataKey="ts"
                type="number"
                domain={['dataMin', 'dataMax']}
                tickFormatter={(v) => formatTick(v, widget.time_range_minutes)}
                stroke="var(--fg-muted)"
                fontSize={10}
              />
              <YAxis stroke="var(--fg-muted)" fontSize={10} width={40} />
              <Tooltip
                contentStyle={{
                  background: 'var(--bg-card)',
                  border: '1px solid var(--border)',
                  borderRadius: 4,
                  fontSize: 12,
                }}
                labelFormatter={(v) => new Date(Number(v)).toLocaleString()}
                formatter={(v: number | string) => [`${v} ${source.unit}`.trim(), 'value']}
              />
              <Line
                type="monotone"
                dataKey="value"
                stroke="var(--accent)"
                strokeWidth={2}
                dot={false}
                isAnimationActive={false}
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
}

function Gauge({ value, unit }: { value: number | null; unit: string }) {
  return (
    <div className="grid h-full place-items-center">
      <div className="text-center">
        <div className="font-mono text-4xl">
          {value == null ? '—' : formatValue(value)}
        </div>
        {unit && <div className="mt-1 text-xs text-fg-muted">{unit}</div>}
      </div>
    </div>
  );
}

// ---- Sources management modal ------------------------------------------

function SourcesModal({ onClose }: { onClose: () => void }) {
  const sources = useStatsSources();
  const create = useCreateStatsSource();
  const del = useDeleteStatsSource();
  const rotate = useRotateStatsSourceToken();
  const [name, setName] = useState('');
  const [unit, setUnit] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [revealed, setRevealed] = useState<number | null>(null);

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    try {
      const out = await create.mutateAsync({ name, unit });
      setRevealed(out.id);
      setName('');
      setUnit('');
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'failed');
    }
  };

  return (
    <Modal title="Stats sources" onClose={onClose} wide>
      <p className="mb-3 text-sm text-fg-muted">
        Each source has a unique ingest token. Push values from any script with
        a single HTTP POST:
      </p>
      <pre className="mb-4 overflow-x-auto rounded border border-border bg-bg-elevated p-3 font-mono text-xs">
{`curl -X POST \\
  -H 'content-type: application/json' \\
  -d '{"value": 1.23}' \\
  http://<your-host>/api/stats/ingest/<token>`}
      </pre>

      <form onSubmit={add} className="mb-4 grid grid-cols-1 gap-3 sm:grid-cols-[1fr_1fr_auto]">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
          placeholder="Name (e.g. CPU temp)"
          maxLength={64}
          className="rounded border border-border bg-bg-elevated px-3 py-2 text-sm outline-none focus:border-accent"
        />
        <input
          value={unit}
          onChange={(e) => setUnit(e.target.value)}
          placeholder="Unit (e.g. °C)"
          maxLength={32}
          className="rounded border border-border bg-bg-elevated px-3 py-2 text-sm outline-none focus:border-accent"
        />
        <button
          type="submit"
          disabled={create.isPending}
          className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
        >
          {create.isPending ? '…' : 'Add'}
        </button>
      </form>

      {err && (
        <div className="mb-3 rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
          {err}
        </div>
      )}

      <div className="rounded-lg border border-border bg-bg-card/40 divide-y divide-border">
        {(sources.data ?? []).length === 0 && (
          <div className="px-4 py-3 text-sm text-fg-muted">No sources yet.</div>
        )}
        {(sources.data ?? []).map((src) => {
          const showToken = revealed === src.id;
          return (
            <div key={src.id} className="px-4 py-3 text-sm">
              <div className="flex items-center gap-3">
                <ModuleIcon name="activity" className="h-4 w-4 text-fg-muted" />
                <div className="flex-1">
                  <div className="font-medium">{src.name}</div>
                  <div className="text-xs text-fg-muted">{src.unit || 'no unit'}</div>
                </div>
                <button
                  onClick={() => setRevealed(showToken ? null : src.id)}
                  className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-elevated"
                >
                  {showToken ? 'Hide token' : 'Show token'}
                </button>
                <button
                  onClick={async () => {
                    if (!confirm(`Rotate token for "${src.name}"? Existing scripts will break.`)) return;
                    const out = await rotate.mutateAsync(src.id);
                    setRevealed(out.id);
                  }}
                  className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-elevated"
                >
                  Rotate
                </button>
                <button
                  onClick={() => {
                    if (!confirm(`Delete "${src.name}"? All its widgets and points will be removed.`)) return;
                    del.mutate(src.id);
                  }}
                  className="rounded border border-danger/40 px-2 py-1 text-xs text-danger hover:bg-danger/10"
                >
                  Delete
                </button>
              </div>
              {showToken && (
                <div className="mt-2 flex items-center gap-2">
                  <code className="flex-1 break-all rounded border border-border bg-bg-elevated px-2 py-1 font-mono text-xs">
                    {src.token}
                  </code>
                  <button
                    onClick={() => navigator.clipboard?.writeText(src.token)}
                    className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-elevated"
                  >
                    Copy
                  </button>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </Modal>
  );
}

// ---- Widget editor modal ------------------------------------------------

function WidgetModal({
  mode,
  widget,
  sources,
  onClose,
}: {
  mode: 'create' | 'edit';
  widget?: StatsWidget;
  sources: StatsSource[];
  onClose: () => void;
}) {
  const create = useCreateStatsWidget();
  const update = useUpdateStatsWidget();
  const del = useDeleteStatsWidget();

  const [name, setName] = useState(widget?.name ?? '');
  const [sourceId, setSourceId] = useState<number>(widget?.source_id ?? sources[0]?.id ?? 0);
  const [kind, setKind] = useState<'line' | 'gauge'>(widget?.kind ?? 'line');
  const [range, setRange] = useState<number>(widget?.time_range_minutes ?? 60);
  const [err, setErr] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    const input = {
      source_id: sourceId,
      name,
      kind,
      time_range_minutes: range,
      position: widget?.position ?? 0,
      config_json: widget?.config_json ?? '{}',
    };
    try {
      if (mode === 'create') {
        await create.mutateAsync(input);
      } else if (widget) {
        await update.mutateAsync({ id: widget.id, input });
      }
      onClose();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'failed');
    }
  };

  const doDelete = async () => {
    if (!widget) return;
    if (!confirm(`Delete widget "${widget.name}"?`)) return;
    await del.mutateAsync(widget.id);
    onClose();
  };

  return (
    <Modal title={mode === 'create' ? 'New widget' : `Edit ${widget?.name}`} onClose={onClose}>
      <form onSubmit={submit} className="space-y-3">
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Title</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            maxLength={80}
            autoFocus
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Source</span>
          <select
            value={sourceId}
            onChange={(e) => setSourceId(Number(e.target.value))}
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
          >
            {sources.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
                {s.unit && ` (${s.unit})`}
              </option>
            ))}
          </select>
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="block">
            <span className="mb-1 block text-xs text-fg-muted">Type</span>
            <select
              value={kind}
              onChange={(e) => setKind(e.target.value as 'line' | 'gauge')}
              className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
            >
              <option value="line">Line chart</option>
              <option value="gauge">Current value</option>
            </select>
          </label>
          <label className="block">
            <span className="mb-1 block text-xs text-fg-muted">Time range</span>
            <select
              value={range}
              onChange={(e) => setRange(Number(e.target.value))}
              className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
            >
              {TIME_RANGES.map((r) => (
                <option key={r.minutes} value={r.minutes}>
                  Last {r.label}
                </option>
              ))}
            </select>
          </label>
        </div>
        {err && (
          <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
            {err}
          </div>
        )}
        <div className="flex items-center justify-between pt-2">
          <div>
            {mode === 'edit' && (
              <button
                type="button"
                onClick={doDelete}
                className="rounded border border-danger/40 px-3 py-2 text-sm text-danger hover:bg-danger/10"
              >
                Delete
              </button>
            )}
          </div>
          <div className="flex gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded border border-border px-4 py-2 text-sm hover:bg-bg-elevated"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={create.isPending || update.isPending}
              className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
            >
              Save
            </button>
          </div>
        </div>
      </form>
    </Modal>
  );
}

// ---- helpers ------------------------------------------------------------

function Modal({
  title,
  children,
  onClose,
  wide,
}: {
  title: string;
  children: React.ReactNode;
  onClose: () => void;
  wide?: boolean;
}) {
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-6">
      <div
        className={
          'w-full rounded-lg border border-border bg-bg-card p-5 ' +
          (wide ? 'max-w-2xl' : 'max-w-lg')
        }
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-bold">{title}</h2>
          <button
            onClick={onClose}
            className="rounded p-1 text-fg-muted hover:bg-bg-elevated hover:text-fg"
            aria-label="Close"
          >
            ✕
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

function formatRange(min: number): string {
  if (min < 60) return `${min}m`;
  if (min < 60 * 24) return `${Math.round(min / 60)}h`;
  return `${Math.round(min / (60 * 24))}d`;
}

function formatValue(v: number): string {
  if (Math.abs(v) >= 1000) return v.toFixed(0);
  if (Math.abs(v) >= 10) return v.toFixed(1);
  return v.toFixed(2);
}

function formatTick(msV: number | string, rangeMin: number): string {
  const d = new Date(Number(msV));
  if (rangeMin >= 24 * 60) {
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  }
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

function timeAgo(ts: number): string {
  const sec = Math.floor(Date.now() / 1000) - ts;
  if (sec < 60) return `${sec}s ago`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`;
  return `${Math.floor(sec / 86400)}d ago`;
}
