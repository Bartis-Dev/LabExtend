'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { AuthShell } from '@/components/auth-shell';
import { fmtBytes, fmtAbsTime } from '@/lib/format';
import { Folder, File as FileIcon, RefreshCw } from 'lucide-react';

// Extract the leader's JSON error message from a caught API error. Falls
// back to the raw exception string. Without this, you only see "API ... → 502"
// which hides the actual AWS / Hetzner reply (AccessDenied, SignatureDoesNotMatch,
// NoSuchBucket etc).
function apiErr(e: unknown): string {
  const err = e as { body?: { error?: string }; message?: string };
  return err?.body?.error ?? err?.message ?? String(e);
}

interface S3Endpoint {
  id: string; name: string; endpoint: string; region: string;
  access_key: string; path_style: boolean; default_bucket: string;
  created_at: number; updated_at: number;
}

interface S3Object {
  key: string; size: number; last_modified: string; is_folder: boolean;
}

export default function S3Page() {
  return <AuthShell><S3 /></AuthShell>;
}

function S3() {
  const [endpoints, setEndpoints] = useState<S3Endpoint[]>([]);
  const [editing, setEditing] = useState<Partial<S3Endpoint & { secret_key: string }> | null>(null);
  const [selected, setSelected] = useState<string>('');
  const [buckets, setBuckets] = useState<string[]>([]);
  const [bucket, setBucket] = useState<string>('');
  const [prefix, setPrefix] = useState<string>('');
  const [objects, setObjects] = useState<S3Object[]>([]);

  const load = async () => {
    try {
      const r = await api<{ endpoints: S3Endpoint[] }>('/api/s3/endpoints');
      setEndpoints(r.endpoints ?? []);
    } catch { /* */ }
  };
  useEffect(() => { load(); }, []);

  const loadBuckets = async (id: string, ep?: S3Endpoint) => {
    setSelected(id); setBucket(''); setObjects([]); setPrefix('');
    try {
      const r = await api<{ buckets: string[]; bucket_scoped?: boolean }>(
        `/api/s3/endpoints/${encodeURIComponent(id)}/buckets`,
      );
      const list = r.buckets ?? [];
      setBuckets(list);
      // Auto-open the bucket when there's only one (typical for bucket-
      // scoped Hetzner / R2 credentials with default_bucket configured).
      if (list.length === 1) {
        loadObjects(list[0], '');
      } else if (ep?.default_bucket && list.includes(ep.default_bucket)) {
        loadObjects(ep.default_bucket, '');
      }
    } catch (e: unknown) {
      // No default_bucket configured AND ListBuckets failed — true error.
      setBuckets([]);
      alert(
        'Bucket-Liste konnte nicht geladen werden.\n\n' +
        apiErr(e) +
        '\n\nFix: Edit endpoint → "Default bucket (optional)" → trage deinen Bucket-Namen ein.',
      );
    }
  };

  const loadObjects = async (b: string, pref: string) => {
    if (!selected || !b) return;
    setBucket(b); setPrefix(pref);
    try {
      const r = await api<{ objects: S3Object[] }>(
        `/api/s3/endpoints/${encodeURIComponent(selected)}/buckets/${encodeURIComponent(b)}/objects?prefix=${encodeURIComponent(pref)}`
      );
      setObjects(r.objects ?? []);
    } catch (e: unknown) { alert('failed: ' + apiErr(e)); }
  };

  const save = async (e: Partial<S3Endpoint & { secret_key: string }>) => {
    const payload = {
      name: e.name ?? '', endpoint: e.endpoint ?? '', region: e.region || 'us-east-1',
      access_key: e.access_key ?? '', secret_key: e.secret_key ?? '',
      path_style: e.path_style ?? true, default_bucket: e.default_bucket ?? '',
    };
    if (e.id) await api(`/api/s3/endpoints/${e.id}`, { method: 'PUT', body: payload });
    else await api('/api/s3/endpoints', { method: 'POST', body: payload });
    setEditing(null);
    load();
  };
  const remove = async (id: string) => {
    if (!confirm('Delete endpoint?')) return;
    await api(`/api/s3/endpoints/${id}`, { method: 'DELETE' });
    load();
  };
  const test = async (id: string) => {
    try {
      const r = await api<{ ok: boolean; bucket_count?: number; tested_bucket?: string }>(
        `/api/s3/endpoints/${encodeURIComponent(id)}/test`, { method: 'POST' });
      if (r.tested_bucket) {
        alert(`OK — bucket "${r.tested_bucket}" reachable (ListBuckets not permitted by these credentials — that's normal for Hetzner).`);
      } else {
        alert(`OK — ${r.bucket_count ?? 0} bucket(s) visible.`);
      }
    } catch (e: unknown) { alert('failed: ' + apiErr(e)); }
  };

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Object storage</h1>
          <p className="text-xs text-zinc-500">Hetzner Object Storage + any S3-compatible endpoint.</p>
        </div>
        <button onClick={() => setEditing({ path_style: true, region: 'us-east-1' })} className="btn-primary">Add endpoint</button>
      </header>

      <div className="card mb-4 overflow-hidden p-0">
        <table className="w-full text-sm">
          <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
            <tr>
              <th className="px-4 py-2">Name</th>
              <th className="px-4 py-2">Endpoint</th>
              <th className="px-4 py-2">Region</th>
              <th className="px-4 py-2">Access key</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {endpoints.map((e) => (
              <tr key={e.id} className={`border-b border-zinc-100 last:border-0 dark:border-zinc-800 ${selected === e.id ? 'bg-zinc-50 dark:bg-zinc-900' : ''}`}>
                <td className="px-4 py-2 font-medium">{e.name}</td>
                <td className="px-4 py-2 font-mono text-[11px]">{e.endpoint}</td>
                <td className="px-4 py-2 text-zinc-500">{e.region}</td>
                <td className="px-4 py-2 font-mono text-[11px]">{e.access_key}</td>
                <td className="px-4 py-2 text-right">
                  <button onClick={() => loadBuckets(e.id, e)} className="btn-ghost">browse</button>
                  <button onClick={() => test(e.id)} className="btn-ghost">test</button>
                  <button onClick={() => setEditing({ ...e, secret_key: '' })} className="btn-ghost">edit</button>
                  <button onClick={() => remove(e.id)} className="btn-ghost text-red-600">delete</button>
                </td>
              </tr>
            ))}
            {endpoints.length === 0 && <tr><td colSpan={5} className="px-4 py-6 text-center text-zinc-500">No endpoints yet.</td></tr>}
          </tbody>
        </table>
      </div>

      {selected && (
        <div className="card overflow-hidden p-0">
          <div className="flex flex-wrap items-center gap-2 border-b border-zinc-200 px-4 py-2 text-xs dark:border-zinc-800">
            {buckets.length > 0 ? (
              <select className="input h-7 w-44 text-xs" value={bucket} onChange={(e) => loadObjects(e.target.value, '')}>
                <option value="">— bucket —</option>
                {buckets.map((b) => <option key={b} value={b}>{b}</option>)}
              </select>
            ) : (
              <ManualBucketInput onSubmit={(b) => loadObjects(b, '')} />
            )}
            <span className="font-mono text-[11px] text-zinc-500">{bucket}{prefix && '/' + prefix}</span>
            <button onClick={() => loadObjects(bucket, prefix)} className="btn-ghost ml-auto"><RefreshCw className="h-3 w-3" /> refresh</button>
          </div>
          {bucket && (
            <table className="w-full text-sm">
              <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
                <tr>
                  <th className="px-4 py-2">Key</th>
                  <th className="px-4 py-2">Size</th>
                  <th className="px-4 py-2 text-right">Modified</th>
                </tr>
              </thead>
              <tbody>
                {prefix && (
                  <tr className="cursor-pointer border-b border-zinc-100 hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-900"
                      onClick={() => {
                        const parts = prefix.split('/').filter(Boolean);
                        parts.pop();
                        loadObjects(bucket, parts.length ? parts.join('/') + '/' : '');
                      }}>
                    <td colSpan={3} className="px-4 py-2 font-mono text-zinc-500">..</td>
                  </tr>
                )}
                {objects.map((o) => {
                  const name = o.key.startsWith(prefix) ? o.key.slice(prefix.length) : o.key;
                  return (
                    <tr key={o.key} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                      <td className="px-4 py-2" onClick={() => o.is_folder && loadObjects(bucket, o.key)}>
                        <span className="inline-flex items-center gap-2">
                          {o.is_folder ? <Folder className="h-4 w-4 text-amber-500" /> : <FileIcon className="h-4 w-4 text-zinc-500" />}
                          <span className={o.is_folder ? 'cursor-pointer hover:underline' : ''}>{name || o.key}</span>
                        </span>
                      </td>
                      <td className="px-4 py-2 font-mono text-[11px] text-zinc-500">{o.is_folder ? '—' : fmtBytes(o.size)}</td>
                      <td className="px-4 py-2 text-right text-[11px] text-zinc-500">{o.last_modified ? fmtAbsTime(new Date(o.last_modified).getTime()) : '—'}</td>
                    </tr>
                  );
                })}
                {objects.length === 0 && <tr><td colSpan={3} className="px-4 py-6 text-center text-zinc-500">Empty.</td></tr>}
              </tbody>
            </table>
          )}
        </div>
      )}

      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setEditing(null)}>
          <div className="card w-full max-w-md" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-base font-semibold">{editing.id ? 'Edit endpoint' : 'New endpoint'}</h3>
            <div className="space-y-3 text-sm">
              <Field label="Name"><input className="input" value={editing.name ?? ''} onChange={(e) => setEditing({ ...editing, name: e.target.value })} /></Field>
              <Field label="Endpoint URL"><input className="input font-mono text-[12px]" placeholder="https://fsn1.your-objectstorage.com" value={editing.endpoint ?? ''} onChange={(e) => setEditing({ ...editing, endpoint: e.target.value })} /></Field>
              <Field label="Region"><input className="input" value={editing.region ?? 'us-east-1'} onChange={(e) => setEditing({ ...editing, region: e.target.value })} /></Field>
              <Field label="Access key"><input className="input font-mono text-[12px]" value={editing.access_key ?? ''} onChange={(e) => setEditing({ ...editing, access_key: e.target.value })} /></Field>
              <Field label={editing.id ? 'Secret key (leave empty to keep)' : 'Secret key'}>
                <input className="input font-mono text-[12px]" type="password" value={editing.secret_key ?? ''} onChange={(e) => setEditing({ ...editing, secret_key: e.target.value })} />
              </Field>
              <Field label="Default bucket">
                <input className="input font-mono text-[12px]" placeholder="bartisdev-backups" value={editing.default_bucket ?? ''} onChange={(e) => setEditing({ ...editing, default_bucket: e.target.value })} />
                <p className="mt-1 text-[11px] text-zinc-500">
                  Bei Hetzner & Cloudflare R2: <strong>nötig</strong>. Diese Anbieter blockieren ListBuckets per default —
                  ohne diesen Wert kann der UI keinen Bucket anzeigen. Trag genau den Bucket-Namen ein wie er bei deinem Anbieter heißt.
                </p>
              </Field>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={editing.path_style ?? true} onChange={(e) => setEditing({ ...editing, path_style: e.target.checked })} />
                Path-style (Hetzner: yes)
              </label>
            </div>
            <div className="mt-4 flex justify-end gap-2">
              <button onClick={() => setEditing(null)} className="btn-ghost">Cancel</button>
              <button onClick={() => save(editing)} className="btn-primary">Save</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="text-xs text-zinc-500">{label}</span>
      <div className="mt-1">{children}</div>
    </label>
  );
}

// Fallback input shown when ListBuckets is forbidden (typical for Hetzner
// bucket-scoped credentials). User types the bucket name manually.
function ManualBucketInput({ onSubmit }: { onSubmit: (b: string) => void }) {
  const [v, setV] = useState('');
  return (
    <form
      className="flex items-center gap-1"
      onSubmit={(e) => { e.preventDefault(); if (v.trim()) onSubmit(v.trim()); }}
    >
      <input
        className="input h-7 w-52 text-xs font-mono"
        placeholder="bucket name (manual)"
        value={v}
        onChange={(e) => setV(e.target.value)}
      />
      <button type="submit" className="btn-primary h-7 text-xs">open</button>
    </form>
  );
}
