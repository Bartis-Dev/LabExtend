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
  const [onlyDDNS, setOnlyDDNS] = useState(false);

  const providersData = providers.data ?? [];
  const cardsData = cards.data ?? [];
  const totalDDNSRecords = (autos.data ?? []).length;

  return (
    <div className="mx-auto max-w-7xl p-6">
      <header className="mb-6 flex flex-wrap items-center gap-3">
        <h1 className="text-3xl font-bold">DDNS</h1>
        <span className="text-sm text-fg-muted">
          {cardsData.length} zone{cardsData.length === 1 ? '' : 's'}
          {totalDDNSRecords > 0 && (
            <>
              {' · '}
              <span className="text-accent">{totalDDNSRecords}</span> record
              {totalDDNSRecords === 1 ? '' : 's'} on auto-update
            </>
          )}
        </span>
        <div className="flex-1" />
        <label
          className={
            'flex cursor-pointer items-center gap-2 rounded border px-3 py-2 text-sm transition-colors ' +
            (onlyDDNS
              ? 'border-accent bg-accent/10 text-accent'
              : 'border-border text-fg-muted hover:bg-bg-elevated')
          }
        >
          <input
            type="checkbox"
            checked={onlyDDNS}
            onChange={(e) => setOnlyDDNS(e.target.checked)}
            className="accent-accent"
          />
          DDNS records only
        </label>
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
        <div className="grid grid-cols-1 gap-5 xl:grid-cols-2">
          {cardsData.map((c) => (
            <ZoneCard
              key={c.id}
              card={c}
              autoUpdates={(autos.data ?? []).filter((a) => a.card_id === c.id)}
              onlyDDNS={onlyDDNS}
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

// Strip the zone suffix from a record name so the table stays scannable.
// "labextend.example.com" in zone "example.com" → "labextend".
// "example.com" in zone "example.com" → "@" (Cloudflare's apex shorthand).
function stripZone(recordName: string, zoneName: string): string {
  if (!zoneName) return recordName;
  if (recordName === zoneName) return '@';
  if (recordName.toLowerCase().endsWith('.' + zoneName.toLowerCase())) {
    return recordName.slice(0, -zoneName.length - 1);
  }
  return recordName;
}

// Stable display order for record-type sections.
const TYPE_ORDER = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV'];

// Per-type accent colours for the badges. Reused tailwind-ish shades.
function typeBadgeStyle(t: string): { bg: string; fg: string } {
  switch (t) {
    case 'A':
      return { bg: 'rgba(21,128,61,0.18)', fg: 'rgb(74,222,128)' }; // green
    case 'AAAA':
      return { bg: 'rgba(29,78,216,0.18)', fg: 'rgb(96,165,250)' }; // blue
    case 'CNAME':
      return { bg: 'rgba(124,58,237,0.18)', fg: 'rgb(192,132,252)' }; // violet
    case 'MX':
      return { bg: 'rgba(180,83,9,0.20)', fg: 'rgb(251,191,36)' }; // amber
    case 'TXT':
      return { bg: 'rgba(15,118,110,0.18)', fg: 'rgb(94,234,212)' }; // teal
    case 'NS':
      return { bg: 'rgba(162,28,175,0.18)', fg: 'rgb(232,121,249)' }; // fuchsia
    case 'SRV':
      return { bg: 'rgba(185,28,28,0.18)', fg: 'rgb(248,113,113)' }; // red
    default:
      return { bg: 'rgba(100,100,100,0.18)', fg: 'rgb(180,180,180)' };
  }
}

function ZoneCard({
  card,
  autoUpdates,
  onlyDDNS,
  onEdit,
}: {
  card: DDNSCard;
  autoUpdates: DDNSAutoUpdate[];
  onlyDDNS: boolean;
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

  const filtered = useMemo(() => {
    let list = (recs.data ?? []).filter((r) => card.show_types.includes(r.type));
    if (onlyDDNS) list = list.filter((r) => r.id != null && autoMap.has(r.id));
    return list;
  }, [recs.data, card.show_types, onlyDDNS, autoMap]);

  // Group records by type for the sectioned display.
  const grouped = useMemo(() => {
    const out: Record<string, DDNSRecord[]> = {};
    for (const r of filtered) (out[r.type] ??= []).push(r);
    for (const k in out) out[k].sort((a, b) => a.name.localeCompare(b.name));
    return out;
  }, [filtered]);

  const typeKeys = TYPE_ORDER.filter((t) => grouped[t]?.length);
  const ddnsCount = autoUpdates.length;

  return (
    <div className="flex flex-col rounded-xl border border-border bg-bg-card/60 shadow-sm">
      <div className="flex items-center gap-3 rounded-t-xl border-b border-border bg-bg-elevated/30 px-5 py-4">
        <ModuleIcon name="globe" className="h-6 w-6 text-fg-muted" />
        <div className="flex-1 min-w-0">
          <div className="truncate text-lg font-semibold">{card.name}</div>
          <div className="mt-0.5 flex flex-wrap items-center gap-2 text-xs text-fg-muted">
            <span>showing</span>
            {card.show_types.map((t) => {
              const s = typeBadgeStyle(t);
              return (
                <span
                  key={t}
                  className="rounded px-1.5 py-0.5 font-mono text-[10px] font-bold uppercase"
                  style={{ background: s.bg, color: s.fg }}
                >
                  {t}
                </span>
              );
            })}
            {ddnsCount > 0 && (
              <span className="ml-2 rounded bg-accent/15 px-1.5 py-0.5 text-[10px] font-bold uppercase text-accent">
                {ddnsCount} on auto-update
              </span>
            )}
          </div>
        </div>
        <button
          onClick={() => setCreating(true)}
          className="rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent-hover"
        >
          + Record
        </button>
        <button
          onClick={onEdit}
          className="rounded-md border border-border px-3 py-1.5 text-xs hover:bg-bg-elevated"
        >
          Edit
        </button>
      </div>

      <div className="flex flex-col gap-4 p-4">
        {recs.isPending && <div className="text-sm text-fg-muted">Loading records…</div>}
        {recs.error && (
          <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
            {(recs.error as Error).message}
          </div>
        )}
        {!recs.isPending && filtered.length === 0 && (
          <div className="rounded-lg border border-dashed border-border px-4 py-6 text-center text-sm text-fg-muted">
            {onlyDDNS
              ? 'No records on auto-update in this zone.'
              : 'No records of the selected types. Click "+ Record" to add one.'}
          </div>
        )}
        {typeKeys.map((t) => (
          <RecordTypeSection
            key={t}
            type={t}
            zoneName={card.name}
            records={grouped[t]}
            autoMap={autoMap}
            onToggleAuto={(r, enabled) =>
              toggle.mutate({
                record_remote_id: r.id!,
                record_name: r.name,
                record_type: r.type as 'A' | 'AAAA',
                enabled,
              })
            }
            onEdit={(r) => setEditingRec(r)}
            onDelete={(r) => {
              if (!confirm(`Delete ${r.type} record ${stripZone(r.name, card.name)}?`)) return;
              del.mutate(r.id!);
            }}
          />
        ))}
      </div>

      {creating && (
        <RecordEditor
          mode="create"
          zoneName={card.name}
          onClose={() => setCreating(false)}
          onSave={async (rec, autoUpdate) => {
            const created = await create.mutateAsync(rec);
            if (autoUpdate && created?.id && (rec.type === 'A' || rec.type === 'AAAA')) {
              await toggle.mutateAsync({
                record_remote_id: created.id,
                record_name: rec.name,
                record_type: rec.type,
                enabled: true,
              });
            }
            setCreating(false);
          }}
        />
      )}
      {editingRec && (
        <RecordEditor
          mode="edit"
          zoneName={card.name}
          initial={editingRec}
          initialAutoUpdate={!!editingRec.id && autoMap.has(editingRec.id)}
          onClose={() => setEditingRec(null)}
          onSave={async (rec, autoUpdate) => {
            await update.mutateAsync({ recordId: editingRec.id!, input: rec });
            if (rec.type === 'A' || rec.type === 'AAAA') {
              const currentlyOn = !!editingRec.id && autoMap.has(editingRec.id);
              if (autoUpdate !== currentlyOn) {
                await toggle.mutateAsync({
                  record_remote_id: editingRec.id!,
                  record_name: rec.name,
                  record_type: rec.type,
                  enabled: autoUpdate,
                });
              }
            }
            setEditingRec(null);
          }}
        />
      )}
    </div>
  );
}

function RecordTypeSection({
  type,
  zoneName,
  records,
  autoMap,
  onToggleAuto,
  onEdit,
  onDelete,
}: {
  type: string;
  zoneName: string;
  records: DDNSRecord[];
  autoMap: Map<string, DDNSAutoUpdate>;
  onToggleAuto: (r: DDNSRecord, enabled: boolean) => void;
  onEdit: (r: DDNSRecord) => void;
  onDelete: (r: DDNSRecord) => void;
}) {
  const style = typeBadgeStyle(type);
  return (
    <section>
      <div className="mb-2 flex items-baseline gap-2">
        <span
          className="rounded px-2 py-0.5 font-mono text-[11px] font-bold uppercase"
          style={{ background: style.bg, color: style.fg }}
        >
          {type}
        </span>
        <span className="text-[11px] uppercase tracking-wider text-fg-muted">
          {records.length} record{records.length === 1 ? '' : 's'}
        </span>
      </div>
      <ul className="flex flex-col gap-2">
        {records.map((r) => {
          const auto = r.id ? autoMap.get(r.id) : undefined;
          const canAuto = r.type === 'A' || r.type === 'AAAA';
          const sub = stripZone(r.name, zoneName);
          return (
            <li
              key={r.id}
              className="flex items-center gap-3 rounded-lg border border-border bg-bg-card/40 px-4 py-3 text-sm transition-colors hover:border-border-strong"
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate font-mono text-base font-medium">{sub}</span>
                  {r.proxied && (
                    <span className="rounded bg-accent/20 px-1.5 py-0.5 text-[10px] font-bold uppercase text-accent">
                      proxied
                    </span>
                  )}
                  {auto && (
                    <span className="rounded bg-accent/20 px-1.5 py-0.5 text-[10px] font-bold uppercase text-accent">
                      DDNS
                    </span>
                  )}
                </div>
                <div className="mt-0.5 break-all font-mono text-xs text-fg-muted">
                  → {r.content}
                </div>
                {auto && (
                  <div className="mt-1 text-[11px]">
                    {auto.last_error ? (
                      <span className="text-danger">{auto.last_error}</span>
                    ) : auto.last_synced_ip ? (
                      <span className="text-fg-muted">
                        synced <span className="font-mono">{auto.last_synced_ip}</span> at{' '}
                        {formatTime(auto.last_synced_at)}
                      </span>
                    ) : (
                      <span className="text-fg-muted">waiting for first check…</span>
                    )}
                  </div>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-1.5">
                {canAuto && (
                  <button
                    onClick={() => onToggleAuto(r, !auto)}
                    className={
                      'rounded-md px-3 py-1.5 text-xs font-medium transition-colors ' +
                      (auto
                        ? 'border border-accent bg-accent/20 text-accent'
                        : 'border border-border text-fg-muted hover:bg-bg-elevated')
                    }
                    title={auto ? 'Auto-update enabled' : 'Enable auto-update'}
                  >
                    {auto ? 'DDNS on' : 'DDNS off'}
                  </button>
                )}
                <button
                  onClick={() => onEdit(r)}
                  className="rounded-md border border-border px-3 py-1.5 text-xs hover:bg-bg-elevated"
                >
                  Edit
                </button>
                <button
                  onClick={() => onDelete(r)}
                  className="rounded-md border border-danger/40 px-3 py-1.5 text-xs text-danger hover:bg-danger/10"
                >
                  Delete
                </button>
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

// --- Record editor --------------------------------------------------------

function RecordEditor({
  mode,
  zoneName,
  initial,
  initialAutoUpdate,
  onClose,
  onSave,
}: {
  mode: 'create' | 'edit';
  zoneName: string;
  initial?: DDNSRecord;
  initialAutoUpdate?: boolean;
  onClose: () => void;
  onSave: (rec: DDNSRecord, autoUpdate: boolean) => Promise<void>;
}) {
  const [type, setType] = useState<string>(initial?.type ?? 'A');
  // Display the subdomain part only ("@" or "labextend") and reassemble
  // the FQDN on submit. For convenience the user can paste a full FQDN
  // and we'll strip the zone suffix automatically.
  const [name, setName] = useState(stripZone(initial?.name ?? '@', zoneName));
  const [content, setContent] = useState(initial?.content ?? '');
  const [ttl, setTtl] = useState<number>(initial?.ttl ?? 1);
  const [proxied, setProxied] = useState(initial?.proxied ?? false);
  const [autoUpdate, setAutoUpdate] = useState<boolean>(initialAutoUpdate ?? false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const canProxy = type === 'A' || type === 'AAAA' || type === 'CNAME';
  const canAutoUpdate = type === 'A' || type === 'AAAA';

  // When the user changes type away from A/AAAA, auto-update is
  // meaningless — silently clear it.
  useEffect(() => {
    if (!canAutoUpdate && autoUpdate) setAutoUpdate(false);
  }, [canAutoUpdate, autoUpdate]);

  const fqdn = (() => {
    const sub = name.trim();
    if (!sub || sub === '@') return zoneName;
    if (sub.toLowerCase().endsWith('.' + zoneName.toLowerCase()) || sub.toLowerCase() === zoneName.toLowerCase()) {
      return sub;
    }
    return `${sub}.${zoneName}`;
  })();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    if (!fqdn || !content) {
      setErr('Name and content are required.');
      return;
    }
    setBusy(true);
    try {
      await onSave(
        { type, name: fqdn, content, ttl, proxied: canProxy ? proxied : false },
        canAutoUpdate ? autoUpdate : false,
      );
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
          <span className="mb-1 block text-xs text-fg-muted">Subdomain</span>
          <div className="flex items-stretch overflow-hidden rounded border border-border bg-bg-elevated focus-within:border-accent">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="@ or labextend"
              required
              className="flex-1 bg-transparent px-3 py-2 font-mono outline-none"
            />
            <span className="flex items-center border-l border-border bg-bg-card px-3 font-mono text-sm text-fg-muted">
              .{zoneName}
            </span>
          </div>
          <div className="mt-1 truncate text-[11px] text-fg-muted">
            Will be saved as <span className="font-mono">{fqdn || '—'}</span>. Use{' '}
            <code className="font-mono">@</code> for the zone apex.
          </div>
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
            <input
              type="checkbox"
              checked={proxied}
              onChange={(e) => setProxied(e.target.checked)}
              className="accent-accent"
            />
            Proxied through Cloudflare
          </label>
        )}
        {canAutoUpdate && (
          <label
            className={
              'flex items-start gap-2 rounded border px-3 py-2 text-sm transition-colors ' +
              (autoUpdate
                ? 'border-accent bg-accent/10'
                : 'border-border bg-bg-elevated/30')
            }
          >
            <input
              type="checkbox"
              checked={autoUpdate}
              onChange={(e) => setAutoUpdate(e.target.checked)}
              className="mt-0.5 accent-accent"
            />
            <span>
              <span className="font-medium">Auto-update with my public IP</span>
              <span className="mt-0.5 block text-xs text-fg-muted">
                LabExtend checks the public {type === 'AAAA' ? 'IPv6' : 'IPv4'} of this server every
                few minutes and patches this record when it changes.
              </span>
            </span>
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
