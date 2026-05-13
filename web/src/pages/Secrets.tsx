import { useEffect, useMemo, useState } from 'react';
import * as OTPAuth from 'otpauth';
import { ApiError } from '@/api/client';
import {
  useCreateVaultEntry,
  useDeleteVaultEntry,
  useSettings,
  useUpdateVaultEntry,
  useVaultEntries,
  type VaultEntryRow,
} from '@/api/queries';
import { useVault } from '@/store/vault';
import { decryptEntry, encryptEntry, type EntryPayload } from '@/lib/vaultCrypto';
import { ModuleIcon } from '@/components/ModuleIcon';

export default function Secrets() {
  const status = useVault((s) => s.status);
  const loadState = useVault((s) => s.loadState);
  const setAutoLockMs = useVault((s) => s.setAutoLockMs);
  const settings = useSettings();

  // Pull initial state once on mount.
  useEffect(() => {
    loadState().catch(() => undefined);
  }, [loadState]);

  // Sync auto-lock setting → store.
  useEffect(() => {
    const v = settings.data?.vault_auto_lock_minutes;
    const n = v ? Number(v) : 5;
    if (Number.isFinite(n) && n >= 1) setAutoLockMs(n * 60_000);
  }, [settings.data, setAutoLockMs]);

  if (status === 'unknown') {
    return <div className="grid h-full place-items-center text-fg-muted">…</div>;
  }
  if (status === 'uninitialized') return <SetupScreen />;
  if (status === 'locked') return <UnlockScreen />;
  return <VaultUI />;
}

// --- Setup ----------------------------------------------------------------

function SetupScreen() {
  const setup = useVault((s) => s.setup);
  const [p1, setP1] = useState('');
  const [p2, setP2] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    if (p1.length < 12) {
      setErr('Vault password must be at least 12 characters.');
      return;
    }
    if (p1 !== p2) {
      setErr('Passwords do not match.');
      return;
    }
    setBusy(true);
    try {
      await setup(p1);
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'setup failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto max-w-md p-8">
      <div className="mb-6 flex flex-col items-center gap-3 text-center">
        <ModuleIcon name="key-round" className="h-10 w-10 text-fg-muted" />
        <h1 className="text-2xl font-bold">Create your vault</h1>
        <p className="text-sm text-fg-muted">
          Pick a master password. It is never sent to the server — your secrets
          are encrypted in this browser before they leave it. If you forget this
          password, the data cannot be recovered.
        </p>
      </div>
      <form onSubmit={submit} className="space-y-3">
        <PwField label="Master password" value={p1} onChange={setP1} autoFocus />
        <PwField label="Confirm master password" value={p2} onChange={setP2} />
        {err && (
          <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
            {err}
          </div>
        )}
        <button
          type="submit"
          disabled={busy}
          className="w-full rounded bg-accent px-4 py-2 text-white hover:bg-accent-hover disabled:opacity-50"
        >
          {busy ? 'Deriving key…' : 'Create vault'}
        </button>
      </form>
    </div>
  );
}

// --- Unlock ---------------------------------------------------------------

function UnlockScreen() {
  const unlock = useVault((s) => s.unlock);
  const [pw, setPw] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      const ok = await unlock(pw);
      if (!ok) setErr('Wrong master password.');
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'unlock failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto max-w-md p-8">
      <div className="mb-6 flex flex-col items-center gap-3 text-center">
        <ModuleIcon name="lock" className="h-10 w-10 text-fg-muted" />
        <h1 className="text-2xl font-bold">Vault locked</h1>
        <p className="text-sm text-fg-muted">
          Enter your master password to unlock your secrets.
        </p>
      </div>
      <form onSubmit={submit} className="space-y-3">
        <PwField label="Master password" value={pw} onChange={setPw} autoFocus />
        {err && (
          <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
            {err}
          </div>
        )}
        <button
          type="submit"
          disabled={busy}
          className="w-full rounded bg-accent px-4 py-2 text-white hover:bg-accent-hover disabled:opacity-50"
        >
          {busy ? 'Deriving key…' : 'Unlock'}
        </button>
      </form>
    </div>
  );
}

function PwField({
  label,
  value,
  onChange,
  autoFocus,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  autoFocus?: boolean;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs text-fg-muted">{label}</span>
      <input
        type="password"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoFocus={autoFocus}
        required
        className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
      />
    </label>
  );
}

// --- Unlocked UI ----------------------------------------------------------

type DecryptedEntry = { row: VaultEntryRow; payload: EntryPayload };

function VaultUI() {
  const key = useVault((s) => s.key);
  const lock = useVault((s) => s.lock);
  const markActivity = useVault((s) => s.markActivity);
  const entries = useVaultEntries(true);
  const [search, setSearch] = useState('');
  const [decrypted, setDecrypted] = useState<DecryptedEntry[]>([]);
  const [decryptErr, setDecryptErr] = useState<string | null>(null);
  const [open, setOpen] = useState<'create' | { id: number } | null>(null);

  // Wire mouse/keyboard activity to the auto-lock timer.
  useEffect(() => {
    const onActivity = () => markActivity();
    window.addEventListener('mousemove', onActivity);
    window.addEventListener('keydown', onActivity);
    return () => {
      window.removeEventListener('mousemove', onActivity);
      window.removeEventListener('keydown', onActivity);
    };
  }, [markActivity]);

  // Decrypt all entries whenever the list changes.
  useEffect(() => {
    if (!key || !entries.data) {
      setDecrypted([]);
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const out: DecryptedEntry[] = [];
        for (const row of entries.data!) {
          const payload = await decryptEntry(key, row.payload_ciphertext, row.payload_nonce);
          out.push({ row, payload });
        }
        if (!cancelled) {
          setDecrypted(out);
          setDecryptErr(null);
        }
      } catch (e) {
        if (!cancelled) setDecryptErr(e instanceof Error ? e.message : 'decryption failed');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [entries.data, key]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return decrypted;
    return decrypted.filter(
      (d) =>
        d.payload.name.toLowerCase().includes(q) ||
        (d.payload.website_url ?? '').toLowerCase().includes(q) ||
        (d.payload.username ?? '').toLowerCase().includes(q),
    );
  }, [decrypted, search]);

  return (
    <div className="mx-auto max-w-5xl p-6">
      <header className="mb-6 flex items-center gap-3">
        <h1 className="flex-1 text-2xl font-bold">Secrets</h1>
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search…"
          className="w-64 rounded border border-border bg-bg-elevated px-3 py-2 text-sm outline-none focus:border-accent"
        />
        <button
          onClick={() => setOpen('create')}
          className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover"
        >
          + New
        </button>
        <button
          onClick={lock}
          className="rounded border border-border px-3 py-2 text-sm hover:bg-bg-elevated"
        >
          Lock
        </button>
      </header>

      {decryptErr && (
        <div className="mb-4 rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
          {decryptErr}
        </div>
      )}

      {entries.isPending && (
        <div className="grid h-32 place-items-center text-fg-muted">Loading…</div>
      )}

      {!entries.isPending && filtered.length === 0 && (
        <div className="grid h-48 place-items-center text-center text-fg-muted">
          <div>
            <p>No secrets yet.</p>
            <p className="mt-1 text-sm">Click &ldquo;+ New&rdquo; to add your first one.</p>
          </div>
        </div>
      )}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {filtered.map((d) => (
          <EntryCard
            key={d.row.id}
            entry={d}
            onClick={() => setOpen({ id: d.row.id })}
          />
        ))}
      </div>

      {open === 'create' && (
        <EntryEditor mode="create" onClose={() => setOpen(null)} />
      )}
      {open && open !== 'create' && (
        <EntryEditor
          mode="edit"
          entry={decrypted.find((d) => d.row.id === open.id)}
          onClose={() => setOpen(null)}
        />
      )}
    </div>
  );
}

function faviconFor(url?: string): string | null {
  if (!url) return null;
  try {
    const u = new URL(url);
    if (!u.hostname) return null;
    return `https://www.google.com/s2/favicons?domain=${encodeURIComponent(u.hostname)}&sz=64`;
  } catch {
    return null;
  }
}

function EntryCard({ entry, onClick }: { entry: DecryptedEntry; onClick: () => void }) {
  const fav = faviconFor(entry.payload.website_url);
  return (
    <button
      onClick={onClick}
      className="flex items-start gap-3 rounded-lg border border-border bg-bg-card/40 p-4 text-left transition-colors hover:border-border-strong hover:bg-bg-card"
    >
      <div className="grid h-10 w-10 shrink-0 place-items-center rounded bg-bg-elevated">
        {fav ? (
          <img src={fav} alt="" className="h-6 w-6" />
        ) : (
          <ModuleIcon name="key-round" className="h-5 w-5 text-fg-muted" />
        )}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">{entry.payload.name}</div>
        <div className="truncate text-xs text-fg-muted">
          {entry.payload.username || entry.payload.website_url || '—'}
        </div>
      </div>
      {entry.payload.totp_secret && (
        <ModuleIcon name="smartphone" className="h-4 w-4 text-fg-muted" />
      )}
    </button>
  );
}

function EntryEditor({
  mode,
  entry,
  onClose,
}: {
  mode: 'create' | 'edit';
  entry?: DecryptedEntry;
  onClose: () => void;
}) {
  const key = useVault((s) => s.key);
  const create = useCreateVaultEntry();
  const update = useUpdateVaultEntry();
  const del = useDeleteVaultEntry();

  const [name, setName] = useState(entry?.payload.name ?? '');
  const [website, setWebsite] = useState(entry?.payload.website_url ?? '');
  const [username, setUsername] = useState(entry?.payload.username ?? '');
  const [secret, setSecret] = useState(entry?.payload.secret ?? '');
  const [notes, setNotes] = useState(entry?.payload.notes ?? '');
  const [totpSecret, setTotpSecret] = useState(entry?.payload.totp_secret ?? '');
  const [showSecret, setShowSecret] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // Live TOTP code while editing.
  const [totpCode, setTotpCode] = useState<string | null>(null);
  const [totpProgress, setTotpProgress] = useState(0);
  useEffect(() => {
    if (!totpSecret.trim()) {
      setTotpCode(null);
      return;
    }
    let totp: OTPAuth.TOTP;
    try {
      totp = new OTPAuth.TOTP({
        secret: OTPAuth.Secret.fromBase32(totpSecret.trim().replace(/\s/g, '').toUpperCase()),
        digits: 6,
        period: 30,
        algorithm: 'SHA1',
      });
    } catch {
      setTotpCode(null);
      return;
    }
    const tick = () => {
      try {
        setTotpCode(totp.generate());
        const period = 30;
        setTotpProgress(((Date.now() / 1000) % period) / period);
      } catch {
        setTotpCode(null);
      }
    };
    tick();
    const t = setInterval(tick, 1000);
    return () => clearInterval(t);
  }, [totpSecret]);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    if (!key) {
      setErr('vault is not unlocked');
      return;
    }
    if (!name.trim() || !secret.trim()) {
      setErr('Name and secret are required.');
      return;
    }
    setBusy(true);
    try {
      const payload: EntryPayload = {
        name: name.trim(),
        website_url: website.trim() || undefined,
        username: username.trim() || undefined,
        secret,
        notes: notes.trim() || undefined,
        totp_secret: totpSecret.trim() || undefined,
      };
      const encrypted = await encryptEntry(key, payload);
      if (mode === 'create') {
        await create.mutateAsync(encrypted);
      } else if (entry) {
        await update.mutateAsync({ id: entry.row.id, input: encrypted });
      }
      onClose();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : e instanceof Error ? e.message : 'save failed');
    } finally {
      setBusy(false);
    }
  };

  const doDelete = async () => {
    if (!entry) return;
    if (!confirm(`Delete "${entry.payload.name}"? This cannot be undone.`)) return;
    setBusy(true);
    try {
      await del.mutateAsync(entry.row.id);
      onClose();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'delete failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-6">
      <form
        onSubmit={save}
        className="w-full max-w-xl space-y-3 rounded-lg border border-border bg-bg-card p-5"
      >
        <h2 className="mb-1 text-lg font-bold">
          {mode === 'create' ? 'New secret' : entry?.payload.name}
        </h2>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Name" value={name} onChange={setName} required autoFocus />
          <Field label="Username (optional)" value={username} onChange={setUsername} />
          <Field
            label="Website URL (optional)"
            value={website}
            onChange={setWebsite}
            placeholder="https://example.com"
            className="sm:col-span-2"
          />
        </div>

        <div>
          <div className="mb-1 flex items-baseline justify-between">
            <span className="text-xs text-fg-muted">Secret / API key / password</span>
            <button
              type="button"
              onClick={() => setShowSecret((v) => !v)}
              className="text-xs text-fg-muted hover:text-fg"
            >
              {showSecret ? 'Hide' : 'Show'}
            </button>
          </div>
          <textarea
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            rows={2}
            required
            style={{
              WebkitTextSecurity: showSecret ? 'none' : 'disc',
            } as React.CSSProperties}
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 font-mono text-sm outline-none focus:border-accent"
          />
        </div>

        <div>
          <span className="mb-1 block text-xs text-fg-muted">TOTP secret (Base32, optional)</span>
          <input
            value={totpSecret}
            onChange={(e) => setTotpSecret(e.target.value)}
            placeholder="JBSWY3DPEHPK3PXP"
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 font-mono text-sm outline-none focus:border-accent"
          />
          {totpCode && (
            <div className="mt-2 flex items-center gap-3 rounded border border-border bg-bg-elevated/50 px-3 py-2">
              <span className="font-mono text-xl tracking-[0.3em]">{totpCode}</span>
              <div className="h-2 flex-1 overflow-hidden rounded bg-bg-card">
                <div
                  className="h-full bg-accent transition-[width] duration-1000 ease-linear"
                  style={{ width: `${(1 - totpProgress) * 100}%` }}
                />
              </div>
              <button
                type="button"
                onClick={() => navigator.clipboard.writeText(totpCode)}
                className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-card"
              >
                Copy
              </button>
            </div>
          )}
        </div>

        <div>
          <span className="mb-1 block text-xs text-fg-muted">Notes (optional)</span>
          <textarea
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            rows={3}
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 text-sm outline-none focus:border-accent"
          />
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
                disabled={busy}
                className="rounded border border-danger/40 px-3 py-2 text-sm text-danger hover:bg-danger/10 disabled:opacity-50"
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
              disabled={busy}
              className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
            >
              {busy ? '…' : 'Save'}
            </button>
          </div>
        </div>
      </form>
    </div>
  );
}

function Field({
  label,
  value,
  onChange,
  required,
  placeholder,
  autoFocus,
  className,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  required?: boolean;
  placeholder?: string;
  autoFocus?: boolean;
  className?: string;
}) {
  return (
    <label className={`block ${className ?? ''}`}>
      <span className="mb-1 block text-xs text-fg-muted">{label}</span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        required={required}
        placeholder={placeholder}
        autoFocus={autoFocus}
        className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
      />
    </label>
  );
}
