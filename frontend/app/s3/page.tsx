'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { AuthShell } from '@/components/auth-shell';
import { fmtBytes, fmtAbsTime } from '@/lib/format';
import { Folder, File as FileIcon, RefreshCw, Plus, Pencil, Trash2, Wifi } from 'lucide-react';

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
      const r = await api<{ buckets: string[] }>(`/api/s3/endpoints/${encodeURIComponent(id)}/buckets`);
      const list = r.buckets ?? [];
      setBuckets(list);
      const target =
        list.length === 1 ? list[0]
        : ep?.default_bucket && list.includes(ep.default_bucket) ? ep.default_bucket
        : '';
      if (target) loadObjects(target, '', id);
    } catch (e: unknown) {
      setBuckets([]);
      alert('Bucket-Liste konnte nicht geladen werden.\n\n' + apiErr(e) +
        '\n\nFix: Edit endpoint → "Default bucket" → trage deinen Bucket-Namen ein.');
    }
  };

  const loadObjects = async (b: string, pref: string, sid?: string) => {
    const id = sid ?? selected;
    if (!id || !b) return;
    setBucket(b); setPrefix(pref);
    try {
      const r = await api<{ objects: S3Object[] }>(
        `/api/s3/endpoints/${encodeURIComponent(id)}/buckets/${encodeURIComponent(b)}/objects?prefix=${encodeURIComponent(pref)}`,
      );
      setObjects(r.objects ?? []);
    } catch (e: unknown) {
      alert('failed: ' + apiErr(e));
    }
  };

  const save = async (e: Partial<S3Endpoint & { secret_key: string }>) => {
    const payload = {
      name: e.name ?? '', endpoint: e.endpoint ?? '', region: e.region || 'us-east-1',
      access_key: e.access_key ?? '', secret_key: e.secret_key ?? '',
      path_style: e.path_style ?? true, default_bucket: e.default_bucket ?? '',
    };
    if (e.id) await api(`/api/s3/endpoints/${e.id}`, { method: 'PUT', body: payload });
    else      await api('/api/s3/endpoints', { method: 'POST', body: payload });
    setEditing(null);
    load();
  };
  const remove = async (id: string) => {
    if (!confirm('Delete endpoint?')) return;
    await api(`/api/s3/endpoints/${id}`, { method: 'DELETE' });
    if (selected === id) {
      setSelected(''); setBuckets([]); setBucket(''); setObjects([]);
    }
    load();
  };
  const test = async (id: string) => {
    try {
      const r = await api<{ ok: boolean; bucket_count?: number; tested_bucket?: string; write_probe?: string; hint?: string }>(
        `/api/s3/endpoints/${encodeURIComponent(id)}/test`, { method: 'POST' });
      if (r.tested_bucket && r.write_probe === 'ok') {
        alert(`OK — bucket "${r.tested_bucket}" reachable. Read + write both work.`);
      } else if (r.tested_bucket) {
        alert(`OK — bucket "${r.tested_bucket}" reachable (read only verified).`);
      } else if (r.hint) {
        alert(`OK — ${r.bucket_count ?? 0} buckets visible.\n\n${r.hint}`);
      } else {
        alert(`OK — ${r.bucket_count ?? 0} bucket(s) visible.`);
      }
    } catch (e: unknown) { alert('failed: ' + apiErr(e)); }
  };

  const selectedEp = endpoints.find((e) => e.id === selected);

  return (
    <div className="mx-auto max-w-[1600px] px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Object storage</h1>
          <p className="text-xs text-zinc-500">S3-kompatible Endpoints — Hetzner, Cloudflare R2, MinIO, Backblaze, jeder andere.</p>
        </div>
      </header>

      <div className="grid grid-cols-[260px_1fr] gap-4">
        {/* ─── sidebar: endpoint list ─── */}
        <aside className="card sticky top-4 h-fit p-0">
          <div className="flex items-center justify-between border-b border-zinc-200 px-3 py-2 dark:border-zinc-800">
            <span className="text-[11px] font-semibold uppercase tracking-wide text-zinc-500">Endpoints</span>
            <button
              onClick={() => setEditing({ path_style: true, region: 'us-east-1' })}
              className="inline-flex h-6 w-6 items-center justify-center rounded-md text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900 dark:hover:bg-zinc-800 dark:hover:text-zinc-100"
              title="Add endpoint"
            >
              <Plus className="h-4 w-4" />
            </button>
          </div>
          <ul className="max-h-[70vh] overflow-y-auto py-1">
            {endpoints.map((e) => {
              const active = selected === e.id;
              return (
                <li key={e.id}>
                  <button
                    onClick={() => loadBuckets(e.id, e)}
                    className={`group flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-sm hover:bg-zinc-50 dark:hover:bg-zinc-900 ${active ? 'bg-zinc-100 dark:bg-zinc-800' : ''}`}
                  >
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-medium">{e.name}</div>
                      <div className="truncate font-mono text-[10px] text-zinc-500">{e.endpoint.replace(/^https?:\/\//, '')}</div>
                    </div>
                    <div className="hidden gap-1 group-hover:flex">
                      <span
                        role="button"
                        onClick={(ev) => { ev.stopPropagation(); test(e.id); }}
                        className="rounded p-1 text-zinc-400 hover:text-emerald-600"
                        title="test connection"
                      ><Wifi className="h-3.5 w-3.5" /></span>
                      <span
                        role="button"
                        onClick={(ev) => { ev.stopPropagation(); setEditing({ ...e, secret_key: '' }); }}
                        className="rounded p-1 text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100"
                        title="edit"
                      ><Pencil className="h-3.5 w-3.5" /></span>
                      <span
                        role="button"
                        onClick={(ev) => { ev.stopPropagation(); remove(e.id); }}
                        className="rounded p-1 text-zinc-400 hover:text-red-600"
                        title="delete"
                      ><Trash2 className="h-3.5 w-3.5" /></span>
                    </div>
                  </button>
                </li>
              );
            })}
            {endpoints.length === 0 && (
              <li className="px-3 py-6 text-center text-xs text-zinc-500">
                Noch keine Endpoints.<br />
                <button onClick={() => setEditing({ path_style: true, region: 'us-east-1' })} className="mt-2 text-zinc-700 underline dark:text-zinc-300">Ersten anlegen</button>
              </li>
            )}
          </ul>
        </aside>

        {/* ─── main: browser ─── */}
        <main className="card overflow-hidden p-0">
          {!selected ? (
            <div className="flex h-[60vh] items-center justify-center text-center text-sm text-zinc-500">
              {endpoints.length === 0
                ? 'Lege links einen Endpoint an, dann erscheint hier dein Filebrowser.'
                : 'Wähle links einen Endpoint, um den Inhalt zu sehen.'}
            </div>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-zinc-200 px-4 py-2 text-xs dark:border-zinc-800">
                {buckets.length > 0 ? (
                  <select className="input h-7 w-48 text-xs" value={bucket} onChange={(e) => loadObjects(e.target.value, '')}>
                    <option value="">— bucket —</option>
                    {buckets.map((b) => <option key={b} value={b}>{b}</option>)}
                  </select>
                ) : (
                  <ManualBucketInput onSubmit={(b) => loadObjects(b, '')} />
                )}
                <span className="font-mono text-[11px] text-zinc-500">
                  {selectedEp?.name && <span className="text-zinc-400">{selectedEp.name} / </span>}
                  {bucket}{prefix && '/' + prefix}
                </span>
                <button onClick={() => loadObjects(bucket, prefix)} className="btn-ghost ml-auto" disabled={!bucket}>
                  <RefreshCw className="h-3 w-3" /> refresh
                </button>
              </div>
              {bucket ? (
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
              ) : (
                <div className="flex h-64 items-center justify-center text-sm text-zinc-500">
                  Wähle einen Bucket aus dem Dropdown oben.
                </div>
              )}
            </>
          )}
        </main>
      </div>

      {/* ─── editor modal ─── */}
      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setEditing(null)}>
          <div className="card w-full max-w-md" onClick={(e) => e.stopPropagation()}>
            <h3 className="mb-3 text-base font-semibold">{editing.id ? 'Edit endpoint' : 'New endpoint'}</h3>
            <div className="space-y-3 text-sm">
              <Field label="Name"><input className="input" value={editing.name ?? ''} onChange={(e) => setEditing({ ...editing, name: e.target.value })} /></Field>
              <Field label="Endpoint URL">
                <input className="input font-mono text-[12px]" placeholder="https://fsn1.your-objectstorage.com" value={editing.endpoint ?? ''} onChange={(e) => setEditing({ ...editing, endpoint: e.target.value })} />
                <p className="mt-1 text-[11px] text-zinc-500">
                  <strong>Nur host, kein bucket im pfad.</strong> Cloudflare R2 zeigt&nbsp;
                  <code className="text-[10px]">https://&lt;account&gt;.r2.cloudflarestorage.com/&lt;bucket&gt;</code>
                  — den <code className="text-[10px]">/&lt;bucket&gt;</code>-Teil weglassen. Hetzner:&nbsp;
                  <code className="text-[10px]">https://nbg1.your-objectstorage.com</code>
                </p>
              </Field>
              <Field label="Region"><input className="input" value={editing.region ?? 'us-east-1'} onChange={(e) => setEditing({ ...editing, region: e.target.value })} /></Field>
              <Field label="Access key"><input className="input font-mono text-[12px]" value={editing.access_key ?? ''} onChange={(e) => setEditing({ ...editing, access_key: e.target.value })} /></Field>
              <Field label={editing.id ? 'Secret key (leave empty to keep)' : 'Secret key'}>
                <input className="input font-mono text-[12px]" type="password" value={editing.secret_key ?? ''} onChange={(e) => setEditing({ ...editing, secret_key: e.target.value })} />
              </Field>
              <Field label="Default bucket">
                <input className="input font-mono text-[12px]" placeholder="bartisdev-backups" value={editing.default_bucket ?? ''} onChange={(e) => setEditing({ ...editing, default_bucket: e.target.value })} />
                <p className="mt-1 text-[11px] text-zinc-500">
                  Bei Hetzner & Cloudflare R2: <strong>nötig</strong>. Diese Anbieter blockieren ListBuckets per default —
                  ohne diesen Wert kann der UI keinen Bucket anzeigen.
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
