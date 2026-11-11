'use client';

import { useEffect, useState } from 'react';
import { api, errText } from '@/lib/api';
import { AuthShell } from '@/components/auth-shell';
import { fmtBytes, fmtAbsTime } from '@/lib/format';
import { Folder, File as FileIcon, RefreshCw, Plus, Pencil, Trash2, Wifi, HardDrive, FolderOpen } from 'lucide-react';
import { Modal } from '@/components/modal';
import { Field, inputCls } from '@/components/field';
import { confirmDialog } from '@/components/confirm';
import { toast } from '@/components/toast';
import { DelayedSpinner, EmptyState, ErrorState, TableSkeleton, useDelayed } from '@/components/feedback';
import { Tooltip } from '@/components/tooltip';

interface S3Endpoint {
  id: string; name: string; endpoint: string; region: string;
  access_key: string; path_style: boolean; default_bucket: string;
  created_at: number; updated_at: number;
}

interface S3Object {
  key: string; size: number; last_modified: string; is_folder: boolean;
}

type Editing = Partial<S3Endpoint & { secret_key: string }>;

export default function S3Page() {
  return <AuthShell><S3 /></AuthShell>;
}

function S3() {
  const [endpoints, setEndpoints] = useState<S3Endpoint[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [editing, setEditing] = useState<Editing | null>(null);
  const [fieldErr, setFieldErr] = useState<Record<string, string>>({});
  const [formErr, setFormErr] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [selected, setSelected] = useState<string>('');
  const [buckets, setBuckets] = useState<string[]>([]);
  const [bucketErr, setBucketErr] = useState<string | null>(null);
  const [bucket, setBucket] = useState<string>('');
  const [prefix, setPrefix] = useState<string>('');
  const [objects, setObjects] = useState<S3Object[]>([]);
  const [objectsErr, setObjectsErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);
  const slow = useDelayed(busy);

  const load = async () => {
    try {
      const r = await api<{ endpoints: S3Endpoint[] }>('/api/s3/endpoints');
      setEndpoints(r.endpoints ?? []);
      setLoadErr(null);
    } catch (e) {
      setLoadErr(errText(e, 'Could not load S3 endpoints.'));
    } finally {
      setLoaded(true);
    }
  };
  useEffect(() => { load(); }, []);

  const loadBuckets = async (id: string, ep?: S3Endpoint) => {
    setSelected(id); setBucket(''); setObjects([]); setPrefix(''); setBucketErr(null); setObjectsErr(null);
    setBusy(true);
    try {
      const r = await api<{ buckets: string[] }>(`/api/s3/endpoints/${encodeURIComponent(id)}/buckets`);
      const list = r.buckets ?? [];
      setBuckets(list);
      const target =
        list.length === 1 ? list[0]
        : ep?.default_bucket && list.includes(ep.default_bucket) ? ep.default_bucket
        : '';
      if (target) await loadObjects(target, '', id);
    } catch (e: unknown) {
      setBuckets([]);
      setBucketErr(errText(e, 'The bucket list could not be loaded.'));
    } finally {
      setBusy(false);
    }
  };

  const loadObjects = async (b: string, pref: string, sid?: string) => {
    const id = sid ?? selected;
    if (!id || !b) return;
    setBucket(b); setPrefix(pref); setObjectsErr(null);
    setBusy(true);
    try {
      const r = await api<{ objects: S3Object[] }>(
        `/api/s3/endpoints/${encodeURIComponent(id)}/buckets/${encodeURIComponent(b)}/objects?prefix=${encodeURIComponent(pref)}`,
      );
      setObjects(r.objects ?? []);
    } catch (e: unknown) {
      setObjects([]);
      setObjectsErr(errText(e, 'Could not list this bucket.'));
    } finally {
      setBusy(false);
    }
  };

  const openEditor = (e: Editing) => { setFieldErr({}); setFormErr(null); setEditing(e); };

  const save = async (e: Editing) => {
    const errs: Record<string, string> = {};
    if (!e.name?.trim()) errs.name = 'Give the endpoint a name.';
    if (!e.endpoint?.trim()) errs.endpoint = 'Enter the endpoint URL.';
    else if (!/^https?:\/\/[^/]+\/?$/.test(e.endpoint.trim())) errs.endpoint = 'Use http(s)://host only, without a bucket path.';
    if (!e.access_key?.trim()) errs.access_key = 'Enter the access key.';
    if (!e.id && !e.secret_key) errs.secret_key = 'Enter the secret key.';
    setFieldErr(errs);
    if (Object.keys(errs).length) return;

    const payload = {
      name: e.name ?? '', endpoint: (e.endpoint ?? '').trim(), region: e.region || 'us-east-1',
      access_key: e.access_key ?? '', secret_key: e.secret_key ?? '',
      path_style: e.path_style ?? true, default_bucket: e.default_bucket ?? '',
    };
    setSaving(true);
    setFormErr(null);
    try {
      if (e.id) await api(`/api/s3/endpoints/${e.id}`, { method: 'PUT', body: payload });
      else      await api('/api/s3/endpoints', { method: 'POST', body: payload });
      setEditing(null);
      toast.success(`Endpoint ${payload.name} saved.`);
      load();
    } catch (err) {
      setFormErr(errText(err, 'Saving failed.'));
    } finally {
      setSaving(false);
    }
  };
  const remove = async (ep: S3Endpoint) => {
    const ok = await confirmDialog({
      title: `Delete endpoint ${ep.name}?`,
      body: 'Only the connection details are removed. The bucket and its objects stay untouched. Backup plans using this endpoint stop working.',
      confirmLabel: 'Delete endpoint',
      danger: true,
    });
    if (!ok) return;
    try {
      await api(`/api/s3/endpoints/${ep.id}`, { method: 'DELETE' });
      if (selected === ep.id) {
        setSelected(''); setBuckets([]); setBucket(''); setObjects([]);
      }
      toast.success(`Endpoint ${ep.name} deleted.`);
      load();
    } catch (e) {
      toast.error('Delete failed: ' + errText(e, 'no answer from the leader'));
    }
  };
  const test = async (ep: S3Endpoint) => {
    setTesting(ep.id);
    try {
      const r = await api<{ ok: boolean; bucket_count?: number; tested_bucket?: string; write_probe?: string; hint?: string }>(
        `/api/s3/endpoints/${encodeURIComponent(ep.id)}/test`, { method: 'POST' });
      if (r.tested_bucket && r.write_probe === 'ok') {
        toast.success(`${ep.name}: bucket "${r.tested_bucket}" reachable. Read and write both work.`);
      } else if (r.tested_bucket) {
        toast.success(`${ep.name}: bucket "${r.tested_bucket}" reachable (read only verified).`);
      } else if (r.hint) {
        toast.success(`${ep.name}: ${r.bucket_count ?? 0} buckets visible.\n${r.hint}`, 10_000);
      } else {
        toast.success(`${ep.name}: ${r.bucket_count ?? 0} bucket(s) visible.`);
      }
    } catch (e: unknown) {
      toast.error(`${ep.name}: connection test failed. ${errText(e)}`);
    } finally {
      setTesting(null);
    }
  };

  const selectedEp = endpoints.find((e) => e.id === selected);
  const newEndpoint = () => openEditor({ path_style: true, region: 'us-east-1' });

  return (
    <div className="page">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Object storage</h1>
          <p className="page-sub">S3-compatible endpoints: Hetzner, Cloudflare R2, MinIO, Backblaze and any other.</p>
        </div>
        <button onClick={newEndpoint} className="btn-primary"><Plus className="h-4 w-4" /> Add endpoint</button>
      </header>

      {loadErr && <ErrorState message={loadErr} onRetry={load} />}

      {!loaded ? <TableSkeleton rows={4} cols={3} /> : (
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[18rem_1fr] 3xl:grid-cols-[22rem_1fr]">
        {/* ─── endpoint list ─── */}
        <aside className="card h-fit p-0 lg:sticky lg:top-4">
          <div className="flex items-center justify-between border-b border-zinc-200 px-4 py-2.5 dark:border-zinc-800">
            <span className="section-title">Endpoints</span>
          </div>
          <ul className="max-h-[70vh] overflow-y-auto py-1">
            {endpoints.map((e) => {
              const active = selected === e.id;
              return (
                <li key={e.id} className={`group flex items-center gap-1 pr-2 ${active ? 'bg-zinc-100 dark:bg-zinc-800' : 'hover:bg-zinc-50 dark:hover:bg-zinc-800/50'}`}>
                  <button
                    onClick={() => loadBuckets(e.id, e)}
                    aria-current={active ? 'true' : undefined}
                    className="min-w-0 flex-1 px-4 py-2.5 text-left text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent focus-visible:ring-offset-0"
                  >
                    <div className="truncate font-medium">{e.name}</div>
                    <div className="truncate font-mono text-3xs text-zinc-500">{e.endpoint.replace(/^https?:\/\//, '')}</div>
                  </button>
                  <div className="flex shrink-0 gap-0.5 lg:opacity-0 lg:transition-opacity lg:group-focus-within:opacity-100 lg:group-hover:opacity-100">
                    <Tooltip label="Test connection">
                      <button onClick={() => test(e)} disabled={testing === e.id} className="btn-icon h-7 w-7" aria-label={`Test connection to ${e.name}`}>
                        <Wifi className={`h-3.5 w-3.5 ${testing === e.id ? 'motion-safe:animate-pulse' : ''}`} />
                      </button>
                    </Tooltip>
                    <Tooltip label="Edit">
                      <button onClick={() => openEditor({ ...e, secret_key: '' })} className="btn-icon h-7 w-7" aria-label={`Edit ${e.name}`}>
                        <Pencil className="h-3.5 w-3.5" />
                      </button>
                    </Tooltip>
                    <Tooltip label="Delete">
                      <button onClick={() => remove(e)} className="btn-icon h-7 w-7 text-red-600 hover:text-red-700 dark:text-red-400" aria-label={`Delete ${e.name}`}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </Tooltip>
                  </div>
                </li>
              );
            })}
            {endpoints.length === 0 && (
              <li>
                <EmptyState
                  icon={<HardDrive className="h-6 w-6" />}
                  title="No endpoints yet"
                  action={<button onClick={newEndpoint} className="btn-primary btn-sm">Add the first one</button>}
                />
              </li>
            )}
          </ul>
        </aside>

        {/* ─── browser ─── */}
        <section className="card min-w-0 overflow-hidden p-0" aria-label="Bucket browser">
          {!selected ? (
            <EmptyState className="min-h-[50vh] justify-center" icon={<FolderOpen className="h-6 w-6" />} title={endpoints.length === 0 ? 'Add an endpoint to browse it' : 'Pick an endpoint'}>
              {endpoints.length === 0
                ? 'Once an endpoint is saved, its buckets and objects show up here.'
                : 'Select an endpoint on the left to see its buckets.'}
            </EmptyState>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-zinc-200 px-4 py-2.5 text-xs dark:border-zinc-800">
                {buckets.length > 0 ? (
                  <select className="input h-8 w-56 text-xs" aria-label="Bucket" value={bucket} onChange={(e) => loadObjects(e.target.value, '')}>
                    <option value="">Pick a bucket</option>
                    {buckets.map((b) => <option key={b} value={b}>{b}</option>)}
                  </select>
                ) : (
                  <ManualBucketInput onSubmit={(b) => loadObjects(b, '')} />
                )}
                <span className="min-w-0 truncate font-mono text-2xs text-zinc-500">
                  {selectedEp?.name && <span className="text-zinc-400">{selectedEp.name} / </span>}
                  {bucket}{prefix && '/' + prefix}
                </span>
                <span className="ml-auto inline-flex items-center gap-2">
                  <DelayedSpinner active={busy} />
                  <button onClick={() => loadObjects(bucket, prefix)} className="btn-ghost btn-sm" disabled={!bucket || busy}>
                    <RefreshCw className="h-3.5 w-3.5" /> Refresh
                  </button>
                </span>
              </div>
              {bucketErr && (
                <div className="p-4">
                  <ErrorState message={`${bucketErr} Many providers (Hetzner, Cloudflare R2) block listing buckets. Edit the endpoint and set its Default bucket, or type the bucket name above.`} />
                </div>
              )}
              {objectsErr && <div className="p-4"><ErrorState message={objectsErr} onRetry={() => loadObjects(bucket, prefix)} /></div>}
              {bucket ? (
                <div className="overflow-x-auto">
                <table className={`w-full min-w-[36rem] text-sm transition-opacity ${slow ? 'opacity-60' : ''}`}>
                  <thead className="border-b border-zinc-200 dark:border-zinc-800">
                    <tr>
                      <th className="th">Key</th>
                      <th className="th">Size</th>
                      <th className="th text-right">Modified</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800">
                    {prefix && (
                      <tr>
                        <td colSpan={3} className="p-0">
                          <button
                            className="w-full px-4 py-2.5 text-left font-mono text-zinc-500 transition hover:bg-zinc-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent focus-visible:ring-offset-0 dark:hover:bg-zinc-800/50"
                            onClick={() => {
                              const parts = prefix.split('/').filter(Boolean);
                              parts.pop();
                              loadObjects(bucket, parts.length ? parts.join('/') + '/' : '');
                            }}
                          >.. <span className="sr-only">up one level</span></button>
                        </td>
                      </tr>
                    )}
                    {objects.map((o) => {
                      const name = o.key.startsWith(prefix) ? o.key.slice(prefix.length) : o.key;
                      return (
                        <tr key={o.key}>
                          <td className="td">
                            {o.is_folder ? (
                              <button onClick={() => loadObjects(bucket, o.key)} className="inline-flex items-center gap-2 rounded font-medium hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-accent">
                                <Folder className="h-4 w-4 text-amber-500" aria-hidden />{name || o.key}
                              </button>
                            ) : (
                              <span className="inline-flex items-center gap-2">
                                <FileIcon className="h-4 w-4 text-zinc-500" aria-hidden />{name || o.key}
                              </span>
                            )}
                          </td>
                          <td className="td font-mono text-2xs text-zinc-500">{o.is_folder ? '' : fmtBytes(o.size)}</td>
                          <td className="td text-right text-2xs text-zinc-500">{o.last_modified ? fmtAbsTime(new Date(o.last_modified).getTime()) : ''}</td>
                        </tr>
                      );
                    })}
                    {objects.length === 0 && !busy && !objectsErr && (
                      <tr><td colSpan={3}><EmptyState title="This folder is empty" /></td></tr>
                    )}
                  </tbody>
                </table>
                </div>
              ) : !bucketErr && (
                <EmptyState className="min-h-[16rem] justify-center" title={busy ? 'Loading buckets' : 'Pick a bucket'}>
                  {busy ? <DelayedSpinner active /> : 'Choose a bucket from the dropdown above.'}
                </EmptyState>
              )}
            </>
          )}
        </section>
      </div>
      )}

      {/* ─── editor ─── */}
      {editing && (
        <Modal
          title={editing.id ? 'Edit endpoint' : 'New endpoint'}
          onClose={() => setEditing(null)}
          dismissable={!saving}
          footer={
            <>
              <button type="button" onClick={() => setEditing(null)} disabled={saving} className="btn-ghost">Cancel</button>
              <button type="submit" form="s3-form" disabled={saving} className="btn-primary">{saving ? 'Saving…' : 'Save'}</button>
            </>
          }
        >
          <form id="s3-form" noValidate onSubmit={(e) => { e.preventDefault(); save(editing); }} className="space-y-4 text-sm">
            <Field label="Name" error={fieldErr.name}>
              <input className={inputCls(fieldErr.name)} value={editing.name ?? ''} onChange={(e) => setEditing({ ...editing, name: e.target.value })} />
            </Field>
            <Field
              label="Endpoint URL"
              error={fieldErr.endpoint}
              hint={<>
                <strong>Host only, no bucket in the path.</strong> Cloudflare R2 shows{' '}
                <code className="text-3xs">https://&lt;account&gt;.r2.cloudflarestorage.com/&lt;bucket&gt;</code>;
                leave out the <code className="text-3xs">/&lt;bucket&gt;</code> part. Hetzner:{' '}
                <code className="text-3xs">https://nbg1.your-objectstorage.com</code>
              </>}
            >
              <input className={inputCls(fieldErr.endpoint, 'font-mono text-xs')} placeholder="https://fsn1.your-objectstorage.com" value={editing.endpoint ?? ''} onChange={(e) => setEditing({ ...editing, endpoint: e.target.value })} />
            </Field>
            <Field label="Region">
              <input className="input" value={editing.region ?? 'us-east-1'} onChange={(e) => setEditing({ ...editing, region: e.target.value })} />
            </Field>
            <Field label="Access key" error={fieldErr.access_key}>
              <input className={inputCls(fieldErr.access_key, 'font-mono text-xs')} autoComplete="off" value={editing.access_key ?? ''} onChange={(e) => setEditing({ ...editing, access_key: e.target.value })} />
            </Field>
            <Field label={editing.id ? 'Secret key (leave empty to keep)' : 'Secret key'} error={fieldErr.secret_key}>
              <input className={inputCls(fieldErr.secret_key, 'font-mono text-xs')} type="password" autoComplete="new-password" value={editing.secret_key ?? ''} onChange={(e) => setEditing({ ...editing, secret_key: e.target.value })} />
            </Field>
            <Field
              label="Default bucket"
              hint={<>Required for Hetzner and Cloudflare R2. They block ListBuckets by default, so without this value the browser cannot show a bucket.</>}
            >
              <input className="input font-mono text-xs" placeholder="bartisdev-backups" value={editing.default_bucket ?? ''} onChange={(e) => setEditing({ ...editing, default_bucket: e.target.value })} />
            </Field>
            <label className="flex cursor-pointer items-center gap-2 text-sm">
              <input type="checkbox" checked={editing.path_style ?? true} onChange={(e) => setEditing({ ...editing, path_style: e.target.checked })} />
              Path-style addressing (Hetzner: yes)
            </label>
            {formErr && <p role="alert" className="alert-error">{formErr}</p>}
          </form>
        </Modal>
      )}
    </div>
  );
}

function ManualBucketInput({ onSubmit }: { onSubmit: (b: string) => void }) {
  const [v, setV] = useState('');
  return (
    <form
      className="flex items-center gap-1.5"
      noValidate
      onSubmit={(e) => { e.preventDefault(); if (v.trim()) onSubmit(v.trim()); }}
    >
      <input
        className="input h-8 w-56 font-mono text-xs"
        aria-label="Bucket name"
        placeholder="bucket name"
        value={v}
        onChange={(e) => setV(e.target.value)}
      />
      <button type="submit" disabled={!v.trim()} className="btn-primary btn-sm">Open</button>
    </form>
  );
}
