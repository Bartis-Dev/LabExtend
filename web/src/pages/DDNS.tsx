import { useEffect, useMemo, useState } from 'react';
import { ApiError } from '@/api/client';
import {
  useAutoUpdates,
  useCardRecords,
  useCreateCardRecord,
  useCreateDDNSCard,
  useCreateDDNSProvider,
  useDDNSCards,
  useDDNSProviders,
  useDDNSZones,
  useDeleteCardRecord,
  useDeleteDDNSCard,
  useDeleteDDNSProvider,
  useToggleAutoUpdate,
  useUpdateCardRecord,
  useUpdateDDNSCard,
} from '@/api/queries';
import type {
  DDNSAutoUpdate,
  DDNSCard,
  DDNSProvider,
  DDNSRecord,
} from '@/api/types';
import { ModuleIcon } from '@/components/ModuleIcon';

const RECORD_TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV'] as const;
type RecordType = (typeof RECORD_TYPES)[number];

export default function DDNS() {
  const providers = useDDNSProviders();
  const cards = useDDNSCards();
  const autos = useAutoUpdates();
  const [providersOpen, setProvidersOpen] = useState(false);
  const [addCardOpen, setAddCardOpen] = useState(false);
  const [editingCard, setEditingCard] = useState<DDNSCard | null>(null);

  const providersData = providers.data ?? [];
  const cardsData = cards.data ?? [];

  return (
    <div className="mx-auto max-w-7xl p-6">
      <header className="mb-6 flex items-center gap-3">
        <h1 className="flex-1 text-2xl font-bold">DDNS</h1>
        <button
          onClick={() => setProvidersOpen(true)}
          className="rounded border border-border px-3 py-2 text-sm hover:bg-bg-elevated"
        >
          Providers ({providersData.length})
        </button>
        <button
          onClick={() => setAddCardOpen(true)}
          disabled={providersData.length === 0}
          className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
          title={providersData.length === 0 ? 'Add a provider first' : ''}
        >
          + Add zone
        </button>
      </header>

      {providersData.length === 0 && (
        <div className="mb-6 rounded-lg border border-warning/40 bg-warning/10 p-4 text-sm">
          You haven&apos;t connected a DDNS provider yet. Click
          &ldquo;Providers&rdquo; to add a Cloudflare API token.
        </div>
      )}

      {cards.isPending ? (
        <div className="grid h-40 place-items-center text-fg-muted">Loading…</div>
      ) : cardsData.length === 0 ? (
        <div className="grid h-48 place-items-center rounded-lg border border-dashed border-border bg-bg-card/30 text-center text-fg-muted">
          <div>
            <p>No zones yet.</p>
            <p className="mt-1 text-sm">Click &ldquo;+ Add zone&rdquo; to pin one.</p>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2 xl:grid-cols-3">
          {cardsData.map((c) => (
            <ZoneCard
              key={c.id}
              card={c}
              autoUpdates={(autos.data ?? []).filter((a) => a.card_id === c.id)}
              onEdit={() => setEditingCard(c)}
            />
          ))}
        </div>
      )}

      {providersOpen && <ProvidersModal onClose={() => setProvidersOpen(false)} />}
      {addCardOpen && (
        <AddZoneModal
          providers={providersData}
          onClose={() => setAddCardOpen(false)}
        />
      )}
      {editingCard && (
        <EditCardModal card={editingCard} onClose={() => setEditingCard(null)} />
      )}
    </div>
  );
}

// --- Providers modal ------------------------------------------------------

function ProvidersModal({ onClose }: { onClose: () => void }) {
  const providers = useDDNSProviders();
  const create = useCreateDDNSProvider();
  const del = useDeleteDDNSProvider();
  const [name, setName] = useState('');
  const [token, setToken] = useState('');
  const [err, setErr] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    try {
      await create.mutateAsync({ name: name.trim(), kind: 'cloudflare', token: token.trim() });
      setName('');
      setToken('');
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'failed');
    }
  };

  return (
    <Modal onClose={onClose} title="DDNS providers" wide>
      <p className="mb-4 text-sm text-fg-muted">
        Add a Cloudflare API token (scoped to Zone.DNS:Edit on the zones you
        want to manage). The token is verified before saving and stored
        AES-GCM-encrypted with a key derived from the server&apos;s session
        secret.
      </p>
      <form
        onSubmit={submit}
        className="mb-6 grid grid-cols-1 gap-3 rounded-lg border border-border bg-bg-card/40 p-4 sm:grid-cols-[1fr_2fr_auto]"
      >
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
          placeholder="Label (e.g. Personal CF)"
          maxLength={64}
          className="rounded border border-border bg-bg-elevated px-3 py-2 text-sm outline-none focus:border-accent"
        />
        <input
          value={token}
          onChange={(e) => setToken(e.target.value)}
          required
          placeholder="Cloudflare API token"
          className="rounded border border-border bg-bg-elevated px-3 py-2 font-mono text-sm outline-none focus:border-accent"
        />
        <button
          type="submit"
          disabled={create.isPending}
          className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
        >
          {create.isPending ? 'Verifying…' : 'Add'}
        </button>
      </form>
      {err && (
        <div className="mb-4 rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
          {err}
        </div>
      )}

      <div className="rounded-lg border border-border bg-bg-card/40 divide-y divide-border">
        {(providers.data ?? []).length === 0 && (
          <div className="px-4 py-3 text-sm text-fg-muted">No providers yet.</div>
        )}
        {(providers.data ?? []).map((p) => (
          <div key={p.id} className="flex items-center gap-3 px-4 py-3">
            <ModuleIcon name="globe" className="h-5 w-5 text-fg-muted" />
            <div className="flex-1">
              <div className="font-medium">{p.name}</div>
              <div className="text-xs text-fg-muted">{p.kind}</div>
            </div>
            <button
              onClick={() => {
                if (!confirm(`Delete "${p.name}"? Pinned zones will also be removed.`)) return;
                del.mutate(p.id);
              }}
              className="rounded border border-danger/40 px-3 py-1 text-xs text-danger hover:bg-danger/10"
            >
              Delete
            </button>
          </div>
        ))}
      </div>
    </Modal>
  );
}

// --- Add zone modal -------------------------------------------------------

function AddZoneModal({
  providers,
  onClose,
}: {
  providers: DDNSProvider[];
  onClose: () => void;
}) {
  const [providerId, setProviderId] = useState<number | null>(providers[0]?.id ?? null);
  const zones = useDDNSZones(providerId);
  const create = useCreateDDNSCard();
  const [selected, setSelected] = useState<string | null>(null);
  const [types, setTypes] = useState<RecordType[]>(['A', 'AAAA']);
  const [err, setErr] = useState<string | null>(null);

  const toggleType = (t: RecordType) =>
    setTypes((prev) => (prev.includes(t) ? prev.filter((x) => x !== t) : [...prev, t]));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    if (!providerId || !selected) {
      setErr('Pick a provider and a zone.');
      return;
    }
    const zone = (zones.data ?? []).find((z) => z.id === selected);
    if (!zone) {
      setErr('Zone not in the loaded list.');
      return;
    }
    if (types.length === 0) {
      setErr('Pick at least one record type to display.');
      return;
    }
    try {
      await create.mutateAsync({
        provider_id: providerId,
        remote_id: zone.id,
        name: zone.name,
        show_types: types,
      });
      onClose();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'failed');
    }
  };

  return (
    <Modal onClose={onClose} title="Add zone">
      <form onSubmit={submit} className="space-y-4">
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Provider</span>
          <select
            value={providerId ?? ''}
            onChange={(e) => {
              setProviderId(Number(e.target.value));
              setSelected(null);
            }}
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
          >
            {providers.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>

        <div>
          <span className="mb-1 block text-xs text-fg-muted">Zone</span>
          <div className="max-h-56 overflow-y-auto rounded border border-border bg-bg-elevated">
            {zones.isPending && providerId != null && (
              <div className="px-3 py-3 text-sm text-fg-muted">Loading zones from Cloudflare…</div>
            )}
            {zones.error && (
              <div className="px-3 py-3 text-sm text-danger">
                {(zones.error as Error).message}
              </div>
            )}
            {(zones.data ?? []).length === 0 && !zones.isPending && !zones.error && (
              <div className="px-3 py-3 text-sm text-fg-muted">No zones found for this token.</div>
            )}
            {(zones.data ?? []).map((z) => (
              <button
                type="button"
                key={z.id}
                onClick={() => setSelected(z.id)}
                className={
                  'flex w-full items-center justify-between px-3 py-2 text-left text-sm transition-colors ' +
                  (selected === z.id ? 'bg-accent/20 text-fg' : 'hover:bg-bg-card')
                }
              >
                <span className="font-mono">{z.name}</span>
                {selected === z.id && <span className="text-xs text-accent">selected</span>}
              </button>
            ))}
          </div>
        </div>

        <div>
          <span className="mb-1 block text-xs text-fg-muted">Show record types</span>
          <div className="flex flex-wrap gap-2">
            {RECORD_TYPES.map((t) => (
              <label key={t} className="flex items-center gap-1.5 rounded border border-border bg-bg-elevated px-2 py-1 text-xs">
                <input
                  type="checkbox"
                  checked={types.includes(t)}
                  onChange={() => toggleType(t)}
                />
                {t}
              </label>
            ))}
          </div>
        </div>

        {err && (
          <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
            {err}
          </div>
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
            disabled={create.isPending}
            className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
          >
            {create.isPending ? '…' : 'Pin zone'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

// --- Edit card modal ------------------------------------------------------

function EditCardModal({ card, onClose }: { card: DDNSCard; onClose: () => void }) {
  const update = useUpdateDDNSCard();
  const del = useDeleteDDNSCard();
  const [name, setName] = useState(card.name);
  const [types, setTypes] = useState<RecordType[]>(card.show_types as RecordType[]);
  const [err, setErr] = useState<string | null>(null);

  const toggleType = (t: RecordType) =>
    setTypes((prev) => (prev.includes(t) ? prev.filter((x) => x !== t) : [...prev, t]));

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    try {
      await update.mutateAsync({
        id: card.id,
        input: { name, show_types: types, layout: card.layout },
      });
      onClose();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'failed');
    }
  };

  return (
    <Modal onClose={onClose} title={`Edit ${card.name}`}>
      <form onSubmit={save} className="space-y-4">
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Display name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
          />
        </label>
        <div>
          <span className="mb-1 block text-xs text-fg-muted">Show record types</span>
          <div className="flex flex-wrap gap-2">
            {RECORD_TYPES.map((t) => (
              <label key={t} className="flex items-center gap-1.5 rounded border border-border bg-bg-elevated px-2 py-1 text-xs">
                <input type="checkbox" checked={types.includes(t)} onChange={() => toggleType(t)} />
                {t}
              </label>
            ))}
          </div>
        </div>
        {err && (
          <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
            {err}
          </div>
        )}
        <div className="flex items-center justify-between pt-2">
          <button
            type="button"
            onClick={async () => {
              if (!confirm(`Unpin "${card.name}"?`)) return;
              await del.mutateAsync(card.id);
              onClose();
            }}
            className="rounded border border-danger/40 px-3 py-2 text-sm text-danger hover:bg-danger/10"
          >
            Unpin zone
          </button>
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
              disabled={update.isPending}
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

// --- Zone card -----------------------------------------------------------

function ZoneCard({
  card,
  autoUpdates,
  onEdit,
}: {
  card: DDNSCard;
  autoUpdates: DDNSAutoUpdate[];
  onEdit: () => void;
}) {
  const recs = useCardRecords(card.id);
  const create = useCreateCardRecord(card.id);
  const update = useUpdateCardRecord(card.id);
  const del = useDeleteCardRecord(card.id);
  const toggle = useToggleAutoUpdate(card.id);
  const [editingRec, setEditingRec] = useState<DDNSRecord | null>(null);
  const [creating, setCreating] = useState(false);

  const autoMap = useMemo(() => {
    const m = new Map<string, DDNSAutoUpdate>();
    for (const a of autoUpdates) m.set(a.record_remote_id, a);
    return m;
  }, [autoUpdates]);

  const filtered = useMemo(
    () => (recs.data ?? []).filter((r) => card.show_types.includes(r.type)),
    [recs.data, card.show_types],
  );

  return (
    <div className="flex flex-col rounded-lg border border-border bg-bg-card/40">
      <div className="flex items-center gap-2 border-b border-border px-4 py-3">
        <ModuleIcon name="globe" className="h-4 w-4 text-fg-muted" />
        <div className="flex-1 min-w-0">
          <div className="truncate font-medium">{card.name}</div>
          <div className="truncate text-xs text-fg-muted">{card.show_types.join(' · ')}</div>
        </div>
        <button
          onClick={() => setCreating(true)}
          className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-elevated"
        >
          + Record
        </button>
        <button
          onClick={onEdit}
          className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-elevated"
        >
          Edit
        </button>
      </div>

      {recs.isPending && <div className="p-4 text-sm text-fg-muted">Loading records…</div>}
      {recs.error && (
        <div className="p-4 text-sm text-danger">{(recs.error as Error).message}</div>
      )}
      {!recs.isPending && filtered.length === 0 && (
        <div className="p-4 text-sm text-fg-muted">
          No records of the selected types. Click &ldquo;+ Record&rdquo; to add one.
        </div>
      )}
      <ul className="divide-y divide-border">
        {filtered.map((r) => {
          const auto = autoMap.get(r.id!);
          const canAuto = r.type === 'A' || r.type === 'AAAA';
          return (
            <li key={r.id} className="flex items-start gap-3 px-4 py-3 text-sm">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="rounded bg-bg-elevated px-1.5 py-0.5 font-mono text-[10px] uppercase text-fg-muted">
                    {r.type}
                  </span>
                  <span className="truncate font-mono">{r.name}</span>
                  {r.proxied && (
                    <span className="rounded bg-accent/20 px-1.5 py-0.5 text-[10px] text-accent">
                      proxied
                    </span>
                  )}
                </div>
                <div className="mt-0.5 truncate font-mono text-xs text-fg-muted">
                  → {r.content}
                </div>
                {auto && (
                  <div className="mt-1 text-[11px] text-fg-muted">
                    {auto.last_error
                      ? <span className="text-danger">{auto.last_error}</span>
                      : auto.last_synced_ip
                        ? `synced ${auto.last_synced_ip} at ${formatTime(auto.last_synced_at)}`
                        : 'waiting for first check…'}
                  </div>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-1">
                {canAuto && (
                  <button
                    onClick={() => {
                      toggle.mutate({
                        record_remote_id: r.id!,
                        record_name: r.name,
                        record_type: r.type as 'A' | 'AAAA',
                        enabled: !auto,
                      });
                    }}
                    className={
                      'rounded px-2 py-1 text-[11px] transition-colors ' +
                      (auto
                        ? 'border border-accent bg-accent/20 text-accent'
                        : 'border border-border text-fg-muted hover:bg-bg-elevated')
                    }
                    title={auto ? 'Auto-update enabled' : 'Enable auto-update'}
                  >
                    auto
                  </button>
                )}
                <button
                  onClick={() => setEditingRec(r)}
                  className="rounded border border-border px-2 py-1 text-[11px] hover:bg-bg-elevated"
                >
                  edit
                </button>
                <button
                  onClick={() => {
                    if (!confirm(`Delete ${r.type} record ${r.name}?`)) return;
                    del.mutate(r.id!);
                  }}
                  className="rounded border border-danger/40 px-2 py-1 text-[11px] text-danger hover:bg-danger/10"
                >
                  del
                </button>
              </div>
            </li>
          );
        })}
      </ul>

      {creating && (
        <RecordEditor
          mode="create"
          onClose={() => setCreating(false)}
          onSave={async (rec) => {
            await create.mutateAsync(rec);
            setCreating(false);
          }}
        />
      )}
      {editingRec && (
        <RecordEditor
          mode="edit"
          initial={editingRec}
          onClose={() => setEditingRec(null)}
          onSave={async (rec) => {
            await update.mutateAsync({ recordId: editingRec.id!, input: rec });
            setEditingRec(null);
          }}
        />
      )}
    </div>
  );
}

// --- Record editor --------------------------------------------------------

function RecordEditor({
  mode,
  initial,
  onClose,
  onSave,
}: {
  mode: 'create' | 'edit';
  initial?: DDNSRecord;
  onClose: () => void;
  onSave: (rec: DDNSRecord) => Promise<void>;
}) {
  const [type, setType] = useState<string>(initial?.type ?? 'A');
  const [name, setName] = useState(initial?.name ?? '');
  const [content, setContent] = useState(initial?.content ?? '');
  const [ttl, setTtl] = useState<number>(initial?.ttl ?? 1);
  const [proxied, setProxied] = useState(initial?.proxied ?? false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const canProxy = type === 'A' || type === 'AAAA' || type === 'CNAME';

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    if (!name || !content) {
      setErr('Name and content are required.');
      return;
    }
    setBusy(true);
    try {
      await onSave({ type, name, content, ttl, proxied: canProxy ? proxied : false });
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal onClose={onClose} title={mode === 'create' ? 'New record' : 'Edit record'}>
      <form onSubmit={submit} className="space-y-3">
        <div className="grid grid-cols-2 gap-3">
          <label className="block">
            <span className="mb-1 block text-xs text-fg-muted">Type</span>
            <select
              value={type}
              onChange={(e) => setType(e.target.value)}
              disabled={mode === 'edit'}
              className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent disabled:opacity-60"
            >
              {RECORD_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </label>
          <label className="block">
            <span className="mb-1 block text-xs text-fg-muted">TTL</span>
            <select
              value={ttl}
              onChange={(e) => setTtl(Number(e.target.value))}
              className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
            >
              <option value={1}>Auto</option>
              <option value={60}>1 minute</option>
              <option value={300}>5 minutes</option>
              <option value={1800}>30 minutes</option>
              <option value={3600}>1 hour</option>
              <option value={86400}>1 day</option>
            </select>
          </label>
        </div>
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="@ or sub.example.com"
            required
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 font-mono outline-none focus:border-accent"
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">
            {type === 'MX'
              ? 'Mailserver (priority + hostname → use 10 mail.example.com)'
              : type === 'TXT'
                ? 'Text value'
                : type === 'AAAA'
                  ? 'IPv6 address'
                  : type === 'A'
                    ? 'IPv4 address'
                    : 'Content'}
          </span>
          <input
            value={content}
            onChange={(e) => setContent(e.target.value)}
            required
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 font-mono outline-none focus:border-accent"
          />
        </label>
        {canProxy && (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={proxied} onChange={(e) => setProxied(e.target.checked)} />
            Proxied through Cloudflare
          </label>
        )}
        {err && (
          <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
            {err}
          </div>
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
    </Modal>
  );
}

// --- helpers --------------------------------------------------------------

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
  // Close on Escape.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

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

function formatTime(ts: number | null): string {
  if (!ts) return '—';
  const d = new Date(ts * 1000);
  return d.toLocaleString();
}
