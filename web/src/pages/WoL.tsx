import { useEffect, useState } from 'react';
import { ApiError } from '@/api/client';
import {
  useCreateWoLTarget,
  useDeleteWoLTarget,
  useUpdateWoLTarget,
  useWakeWoLTarget,
  useWoLStatus,
  useWoLTargets,
} from '@/api/queries';
import type { WoLTarget, WoLTargetInput } from '@/api/types';
import { ModuleIcon } from '@/components/ModuleIcon';

export default function WoL() {
  const targets = useWoLTargets();
  const status = useWoLStatus();
  const [editing, setEditing] = useState<WoLTarget | 'new' | null>(null);

  const data = targets.data ?? [];
  const statusFor = (id: number): 'up' | 'down' | 'unknown' => {
    const v = status.data?.[String(id)];
    return v ?? 'unknown';
  };

  return (
    <div className="mx-auto max-w-4xl p-6">
      <header className="mb-6 flex items-center gap-3">
        <h1 className="flex-1 text-2xl font-bold">Wake on LAN</h1>
        <button
          onClick={() => setEditing('new')}
          className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover"
        >
          + Add target
        </button>
      </header>

      <div className="mb-4 rounded-lg border border-warning/40 bg-warning/10 px-4 py-3 text-sm">
        The magic packet is sent via UDP from the LabExtend container. If the
        container runs in bridge networking, the packet may not reach your LAN —
        use <code className="font-mono">--network host</code> or expose UDP
        broadcast explicitly. Set a <em>ping host</em> below to see whether each
        machine is currently online.
      </div>

      {targets.isPending ? (
        <div className="grid h-40 place-items-center text-fg-muted">Loading…</div>
      ) : data.length === 0 ? (
        <div className="grid h-48 place-items-center rounded-lg border border-dashed border-border bg-bg-card/30 text-center text-fg-muted">
          <div>
            <p>No targets yet.</p>
            <p className="mt-1 text-sm">Click &ldquo;+ Add target&rdquo; to register a machine.</p>
          </div>
        </div>
      ) : (
        <div className="rounded-lg border border-border bg-bg-card/40 divide-y divide-border">
          {data.map((t) => (
            <TargetRow
              key={t.id}
              target={t}
              status={statusFor(t.id)}
              onEdit={() => setEditing(t)}
            />
          ))}
        </div>
      )}

      {editing && (
        <Editor
          mode={editing === 'new' ? 'create' : 'edit'}
          target={editing === 'new' ? undefined : editing}
          onClose={() => setEditing(null)}
        />
      )}
    </div>
  );
}

function StatusDot({ status }: { status: 'up' | 'down' | 'unknown' }) {
  const cls =
    status === 'up'
      ? 'bg-success labx-status-up'
      : status === 'down'
        ? 'bg-danger'
        : 'bg-fg-muted/30';
  const label =
    status === 'up' ? 'Online' : status === 'down' ? 'Offline' : 'Ping not configured';
  return (
    <span
      className={`inline-block h-2.5 w-2.5 shrink-0 rounded-full ${cls}`}
      title={label}
      aria-label={label}
    />
  );
}

function TargetRow({
  target,
  status,
  onEdit,
}: {
  target: WoLTarget;
  status: 'up' | 'down' | 'unknown';
  onEdit: () => void;
}) {
  const wake = useWakeWoLTarget();
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const doWake = async () => {
    setMsg(null);
    setErr(null);
    try {
      await wake.mutateAsync(target.id);
      setMsg('Magic packet sent.');
      setTimeout(() => setMsg(null), 3000);
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'wake failed');
    }
  };

  return (
    <div className="flex items-center gap-4 px-4 py-3">
      <ModuleIcon name="power" className="h-5 w-5 text-fg-muted" />
      <StatusDot status={status} />
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">{target.name}</div>
        <div className="truncate font-mono text-xs text-fg-muted">
          {formatMac(target.mac)} · {target.broadcast_addr}:{target.port}
          {target.ping_host && (
            <span className="ml-2 text-fg-muted/70">
              ping {target.ping_host}:{target.ping_port}
            </span>
          )}
        </div>
        {target.last_error && (
          <div className="mt-1 text-xs text-danger">last error: {target.last_error}</div>
        )}
        {!target.last_error && target.last_sent_at && (
          <div className="mt-1 text-xs text-fg-muted">
            last sent {new Date(target.last_sent_at * 1000).toLocaleString()}
          </div>
        )}
      </div>
      {msg && <span className="text-xs text-success">{msg}</span>}
      {err && <span className="text-xs text-danger">{err}</span>}
      <button
        onClick={doWake}
        disabled={wake.isPending}
        className="rounded bg-accent px-3 py-1.5 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
      >
        {wake.isPending ? '…' : 'Wake'}
      </button>
      <button
        onClick={onEdit}
        className="rounded border border-border px-3 py-1.5 text-sm hover:bg-bg-elevated"
      >
        Edit
      </button>
    </div>
  );
}

function Editor({
  mode,
  target,
  onClose,
}: {
  mode: 'create' | 'edit';
  target?: WoLTarget;
  onClose: () => void;
}) {
  const create = useCreateWoLTarget();
  const update = useUpdateWoLTarget();
  const del = useDeleteWoLTarget();

  const [name, setName] = useState(target?.name ?? '');
  const [mac, setMac] = useState(target?.mac ? formatMac(target.mac) : '');
  const [broadcast, setBroadcast] = useState(target?.broadcast_addr ?? '255.255.255.255');
  const [port, setPort] = useState(String(target?.port ?? 9));
  const [pingHost, setPingHost] = useState(target?.ping_host ?? '');
  const [pingPort, setPingPort] = useState(String(target?.ping_port || 22));
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    const input: WoLTargetInput = {
      name: name.trim(),
      mac,
      broadcast_addr: broadcast.trim() || '255.255.255.255',
      port: Number(port) || 9,
      ping_host: pingHost.trim(),
      ping_port: Number(pingPort) || 22,
    };
    try {
      if (mode === 'create') {
        await create.mutateAsync(input);
      } else if (target) {
        await update.mutateAsync({ id: target.id, input });
      }
      onClose();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'save failed');
    }
  };

  const doDelete = async () => {
    if (!target) return;
    if (!confirm(`Delete "${target.name}"?`)) return;
    try {
      await del.mutateAsync(target.id);
      onClose();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'delete failed');
    }
  };

  const busy = create.isPending || update.isPending || del.isPending;

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-6">
      <form
        onSubmit={submit}
        className="w-full max-w-md space-y-3 rounded-lg border border-border bg-bg-card p-5"
      >
        <h2 className="mb-1 text-lg font-bold">
          {mode === 'create' ? 'New target' : target?.name}
        </h2>
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            maxLength={64}
            autoFocus
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">MAC address</span>
          <input
            value={mac}
            onChange={(e) => setMac(e.target.value)}
            required
            placeholder="AA:BB:CC:DD:EE:FF"
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 font-mono outline-none focus:border-accent"
          />
        </label>
        <div className="grid grid-cols-[1fr_120px] gap-3">
          <label className="block">
            <span className="mb-1 block text-xs text-fg-muted">Broadcast address</span>
            <input
              value={broadcast}
              onChange={(e) => setBroadcast(e.target.value)}
              placeholder="255.255.255.255"
              className="w-full rounded border border-border bg-bg-elevated px-3 py-2 font-mono outline-none focus:border-accent"
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-xs text-fg-muted">UDP Port</span>
            <input
              type="number"
              value={port}
              onChange={(e) => setPort(e.target.value)}
              min={1}
              max={65535}
              placeholder="9"
              className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
            />
          </label>
        </div>

        <div className="rounded border border-border bg-bg-elevated/30 p-3">
          <div className="mb-2 text-xs font-semibold uppercase tracking-wider text-fg-muted">
            Online check (optional)
          </div>
          <p className="mb-2 text-[11px] text-fg-muted">
            LabExtend tries a TCP connect every 10s to display online/offline.
            Use the device&apos;s actual IP/hostname (not the broadcast). Default
            port 22 (SSH). Leave host empty to disable.
          </p>
          <div className="grid grid-cols-[1fr_120px] gap-3">
            <label className="block">
              <span className="mb-1 block text-xs text-fg-muted">Ping host</span>
              <input
                value={pingHost}
                onChange={(e) => setPingHost(e.target.value)}
                placeholder="192.168.1.42 or my-pc.lan"
                className="w-full rounded border border-border bg-bg-card px-3 py-2 font-mono outline-none focus:border-accent"
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs text-fg-muted">TCP Port</span>
              <input
                type="number"
                value={pingPort}
                onChange={(e) => setPingPort(e.target.value)}
                min={1}
                max={65535}
                placeholder="22"
                className="w-full rounded border border-border bg-bg-card px-3 py-2 outline-none focus:border-accent"
              />
            </label>
          </div>
        </div>

        <p className="text-xs text-fg-muted">
          Use your subnet broadcast (e.g. <code>192.168.1.255</code>) instead of
          255.255.255.255 if your router drops the global broadcast.
        </p>
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

function formatMac(mac: string): string {
  if (mac.includes(':') || mac.includes('-')) return mac.toUpperCase();
  const clean = mac.replace(/[^0-9a-fA-F]/g, '');
  if (clean.length !== 12) return mac;
  return (clean.match(/.{2}/g) ?? []).join(':').toUpperCase();
}
