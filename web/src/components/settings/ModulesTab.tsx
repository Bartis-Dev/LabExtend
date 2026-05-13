import { useMemo, useState } from 'react';
import {
  useCreateIframeModule,
  useDeleteModule,
  useModules,
  useUpdateModule,
} from '@/api/queries';
import type { Module } from '@/api/types';
import { ApiError } from '@/api/client';
import { ModuleIcon } from '@/components/ModuleIcon';

function slugify(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 32)
    .replace(/^-+|-+$/g, '');
}

export function ModulesTab() {
  const modules = useModules();
  const update = useUpdateModule();
  const del = useDeleteModule();
  const create = useCreateIframeModule();

  const [err, setErr] = useState<string | null>(null);
  const [editing, setEditing] = useState<Module | null>(null);

  const sorted = useMemo(
    () =>
      (modules.data ?? [])
        .slice()
        .sort((a, b) => a.position - b.position || a.id - b.id),
    [modules.data],
  );
  const builtins = sorted.filter((m) => m.kind === 'builtin');
  const iframes = sorted.filter((m) => m.kind === 'iframe');

  const runUpdate = (id: number, patch: Parameters<typeof update.mutate>[0]['patch']) => {
    setErr(null);
    update.mutate({ id, patch }, { onError: (e) => setErr(e instanceof ApiError ? e.message : 'update failed') });
  };

  const move = (m: Module, dir: -1 | 1) => {
    const idx = sorted.indexOf(m);
    const neighbour = sorted[idx + dir];
    if (!neighbour) return;
    // Swap positions. Use values that already exist to avoid drifting.
    runUpdate(m.id, { position: neighbour.position });
    runUpdate(neighbour.id, { position: m.position });
  };

  return (
    <div className="space-y-8">
      <section>
        <h3 className="mb-3 text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Built-in modules
        </h3>
        <div className="rounded-lg border border-border bg-bg-card/40 divide-y divide-border">
          {builtins.map((m) => (
            <ModuleRow
              key={m.id}
              m={m}
              onToggle={(enabled) => runUpdate(m.id, { enabled })}
              onMoveUp={() => move(m, -1)}
              onMoveDown={() => move(m, 1)}
              onEdit={() => setEditing(m)}
              busy={update.isPending}
              dashboardLocked={m.builtin_key === 'dashboard'}
            />
          ))}
        </div>
      </section>

      <section>
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
            Iframe modules
          </h3>
        </div>
        {iframes.length > 0 && (
          <div className="mb-4 rounded-lg border border-border bg-bg-card/40 divide-y divide-border">
            {iframes.map((m) => (
              <ModuleRow
                key={m.id}
                m={m}
                onToggle={(enabled) => runUpdate(m.id, { enabled })}
                onMoveUp={() => move(m, -1)}
                onMoveDown={() => move(m, 1)}
                onEdit={() => setEditing(m)}
                onDelete={() => {
                  setErr(null);
                  del.mutate(m.id, {
                    onError: (e) => setErr(e instanceof ApiError ? e.message : 'delete failed'),
                  });
                }}
                busy={update.isPending || del.isPending}
              />
            ))}
          </div>
        )}

        <IframeCreateForm
          onCreate={async (input) => {
            setErr(null);
            try {
              await create.mutateAsync(input);
              return true;
            } catch (e) {
              setErr(e instanceof ApiError ? e.message : 'create failed');
              return false;
            }
          }}
          busy={create.isPending}
        />
      </section>

      {err && (
        <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
          {err}
        </div>
      )}

      {editing && (
        <EditModal
          m={editing}
          onClose={() => setEditing(null)}
          onSave={async (patch) => {
            setErr(null);
            try {
              await update.mutateAsync({ id: editing.id, patch });
              setEditing(null);
            } catch (e) {
              setErr(e instanceof ApiError ? e.message : 'update failed');
            }
          }}
          busy={update.isPending}
        />
      )}
    </div>
  );
}

function ModuleRow({
  m,
  onToggle,
  onMoveUp,
  onMoveDown,
  onEdit,
  onDelete,
  busy,
  dashboardLocked,
}: {
  m: Module;
  onToggle: (enabled: boolean) => void;
  onMoveUp: () => void;
  onMoveDown: () => void;
  onEdit: () => void;
  onDelete?: () => void;
  busy: boolean;
  dashboardLocked?: boolean;
}) {
  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <ModuleIcon name={m.icon} className="h-5 w-5 text-fg-muted" />
      <div className="flex-1 min-w-0">
        <div className="truncate font-medium">{m.name}</div>
        <div className="truncate text-xs text-fg-muted">
          /{m.slug === 'dashboard' ? '' : m.slug}
          {m.kind === 'iframe' && m.url ? ` → ${m.url}` : ''}
        </div>
      </div>
      <div className="flex items-center gap-2">
        <button
          onClick={onMoveUp}
          className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-elevated"
          aria-label="Move up"
          disabled={busy}
        >
          ↑
        </button>
        <button
          onClick={onMoveDown}
          className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-elevated"
          aria-label="Move down"
          disabled={busy}
        >
          ↓
        </button>
        <button
          onClick={onEdit}
          className="rounded border border-border px-3 py-1 text-xs hover:bg-bg-elevated"
          disabled={busy}
        >
          Edit
        </button>
        {onDelete && (
          <button
            onClick={onDelete}
            className="rounded border border-danger/40 px-3 py-1 text-xs text-danger hover:bg-danger/10"
            disabled={busy}
          >
            Delete
          </button>
        )}
        <label
          className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
            m.enabled ? 'bg-accent' : 'bg-bg-elevated'
          } ${dashboardLocked ? 'opacity-50' : ''}`}
          title={dashboardLocked ? 'Dashboard cannot be disabled' : ''}
        >
          <input
            type="checkbox"
            className="peer sr-only"
            checked={m.enabled}
            disabled={busy || dashboardLocked}
            onChange={(e) => onToggle(e.target.checked)}
          />
          <span
            className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
              m.enabled ? 'translate-x-6' : 'translate-x-1'
            }`}
          />
        </label>
      </div>
    </div>
  );
}

function IframeCreateForm({
  onCreate,
  busy,
}: {
  onCreate: (input: { name: string; url: string; icon: string; slug?: string }) => Promise<boolean>;
  busy: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [icon, setIcon] = useState('box');

  const slug = slugify(name);

  const reset = () => {
    setName('');
    setUrl('');
    setIcon('box');
  };

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover"
      >
        + Add iframe module
      </button>
    );
  }

  return (
    <form
      onSubmit={async (e) => {
        e.preventDefault();
        const ok = await onCreate({ name, url, icon, slug });
        if (ok) {
          reset();
          setOpen(false);
        }
      }}
      className="space-y-3 rounded-lg border border-border bg-bg-card/40 p-4"
    >
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            maxLength={64}
            placeholder="Home Assistant"
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">URL</span>
          <input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            required
            placeholder="http://homeassistant.local:8123"
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">
            Icon (lucide name, kebab-case)
          </span>
          <div className="flex items-center gap-2">
            <input
              value={icon}
              onChange={(e) => setIcon(e.target.value.trim())}
              placeholder="box"
              className="flex-1 rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
            />
            <div className="grid h-10 w-10 place-items-center rounded border border-border bg-bg-elevated">
              <ModuleIcon name={icon || 'box'} className="h-5 w-5" />
            </div>
          </div>
        </label>
        <div className="block">
          <span className="mb-1 block text-xs text-fg-muted">Slug (auto)</span>
          <div className="rounded border border-border bg-bg-elevated/50 px-3 py-2 font-mono text-sm text-fg-muted">
            /iframe/{slug || '…'}
          </div>
        </div>
      </div>
      <p className="text-xs text-fg-muted/80">
        Many sites refuse embedding via X-Frame-Options or CSP frame-ancestors. If the page is blank
        the target is blocking the iframe — LabExtend cannot work around that.
      </p>
      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={() => {
            reset();
            setOpen(false);
          }}
          className="rounded border border-border px-4 py-2 text-sm hover:bg-bg-elevated"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={busy}
          className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
        >
          {busy ? '…' : 'Create'}
        </button>
      </div>
    </form>
  );
}

function EditModal({
  m,
  onClose,
  onSave,
  busy,
}: {
  m: Module;
  onClose: () => void;
  onSave: (patch: { name?: string; icon?: string; url?: string }) => Promise<void>;
  busy: boolean;
}) {
  const [name, setName] = useState(m.name);
  const [icon, setIcon] = useState(m.icon);
  const [url, setUrl] = useState(m.url ?? '');

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-6">
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          const patch: { name?: string; icon?: string; url?: string } = {};
          if (name !== m.name) patch.name = name;
          if (icon !== m.icon) patch.icon = icon;
          if (m.kind === 'iframe' && url !== (m.url ?? '')) patch.url = url;
          await onSave(patch);
        }}
        className="w-full max-w-md space-y-4 rounded-lg border border-border bg-bg-card p-5"
      >
        <h3 className="text-lg font-bold">Edit {m.name}</h3>
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={64}
            required
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Icon</span>
          <div className="flex items-center gap-2">
            <input
              value={icon}
              onChange={(e) => setIcon(e.target.value.trim())}
              className="flex-1 rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
            />
            <div className="grid h-10 w-10 place-items-center rounded border border-border bg-bg-elevated">
              <ModuleIcon name={icon || 'box'} className="h-5 w-5" />
            </div>
          </div>
        </label>
        {m.kind === 'iframe' && (
          <label className="block">
            <span className="mb-1 block text-xs text-fg-muted">URL</span>
            <input
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              required
              className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
            />
          </label>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-border px-4 py-2 text-sm hover:bg-bg-elevated"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={busy}
            className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
          >
            {busy ? '…' : 'Save'}
          </button>
        </div>
      </form>
    </div>
  );
}
