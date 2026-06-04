'use client';

import { Suspense, useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { api } from '@/lib/api';
import type { NodeView } from '@/lib/types';
import { AuthShell } from '@/components/auth-shell';
import { fmtBytes, fmtAbsTime } from '@/lib/format';
import { Folder, File as FileIcon, ChevronRight, RefreshCw, Trash2, FolderPlus, UserCog } from 'lucide-react';

interface NodePath {
  id: number;
  node_id: string;
  label: string;
  path: string;
  default_uid: number;
  default_gid: number;
  default_user_label?: string;
  read_only: boolean;
  created_at: number;
}

interface FileEntry {
  name: string;
  is_dir: boolean;
  size: number;
  mtime_ms: number;
  mode: number;
  uid: number;
  gid: number;
  owner_name: string;
  group_name: string;
  symlink_target: string;
}

export default function FilesPage() {
  return (
    <AuthShell>
      <Suspense fallback={<div className="p-8 text-sm text-zinc-500">Loading…</div>}>
        <Files />
      </Suspense>
    </AuthShell>
  );
}

function Files() {
  const params = useSearchParams();
  const nodeID = params.get('node') ?? '';
  const root = params.get('root') ?? '';
  const sub = params.get('sub') ?? '';

  const [nodes, setNodes] = useState<NodeView[]>([]);
  const [paths, setPaths] = useState<NodePath[]>([]);
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [showHidden, setShowHidden] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    (async () => {
      try {
        const r = await api<{ nodes: NodeView[] }>('/api/nodes');
        setNodes(r.nodes ?? []);
      } catch { /* */ }
    })();
  }, []);

  useEffect(() => {
    if (!nodeID) return;
    (async () => {
      try {
        const r = await api<{ paths: NodePath[] }>(`/api/nodes/${encodeURIComponent(nodeID)}/paths`);
        setPaths(r.paths ?? []);
      } catch { /* */ }
    })();
  }, [nodeID]);

  const loadEntries = useCallback(async () => {
    if (!nodeID || !root) { setEntries([]); return; }
    setBusy(true);
    setErr(null);
    try {
      const r = await api<{ entries: FileEntry[] }>(
        `/api/nodes/${encodeURIComponent(nodeID)}/files?root=${encodeURIComponent(root)}&sub=${encodeURIComponent(sub)}&show_hidden=${showHidden}`
      );
      setEntries(r.entries ?? []);
    } catch (e: unknown) {
      const er = e as { body?: { error?: string } };
      setErr(er?.body?.error ?? 'failed');
      setEntries([]);
    } finally {
      setBusy(false);
    }
  }, [nodeID, root, sub, showHidden]);

  useEffect(() => { loadEntries(); }, [loadEntries]);

  const goTo = (n: string, dir: boolean) => {
    if (!dir) return;
    const next = sub ? (sub.endsWith('/') ? sub + n : sub + '/' + n) : n;
    window.location.href = `/files?node=${encodeURIComponent(nodeID)}&root=${encodeURIComponent(root)}&sub=${encodeURIComponent(next)}`;
  };

  const goUp = () => {
    if (!sub) return;
    const idx = sub.lastIndexOf('/');
    const next = idx > 0 ? sub.slice(0, idx) : '';
    window.location.href = `/files?node=${encodeURIComponent(nodeID)}&root=${encodeURIComponent(root)}&sub=${encodeURIComponent(next)}`;
  };

  const fullPath = (name: string) => {
    const parts = [root, sub, name].filter(Boolean);
    return '/' + parts.join('/').replace(/^\/+/, '').replace(/\/+/g, '/');
  };

  const onMkdir = async () => {
    const name = prompt('New folder name:');
    if (!name) return;
    try {
      await api(`/api/nodes/${encodeURIComponent(nodeID)}/files/mkdir`, {
        method: 'POST', body: { path: fullPath(name), parents: true, apply_default_owner: true },
      });
      loadEntries();
    } catch (e) { alert('mkdir failed: ' + String(e)); }
  };

  const onDelete = async (e: FileEntry) => {
    if (!confirm(`Delete ${e.name}${e.is_dir ? ' (recursive)' : ''}?`)) return;
    try {
      const q = `path=${encodeURIComponent(fullPath(e.name))}&recursive=${e.is_dir}`;
      await api(`/api/nodes/${encodeURIComponent(nodeID)}/files?${q}`, { method: 'DELETE' });
      loadEntries();
    } catch (er) { alert('delete failed: ' + String(er)); }
  };

  const onChown = async (e: FileEntry) => {
    const owner = prompt('uid:gid (e.g. 1000:1000) — empty = use default');
    if (owner === null) return;
    let uid = 0, gid = 0;
    if (owner) {
      const m = owner.match(/^(\d+):(\d+)$/);
      if (!m) { alert('format: uid:gid'); return; }
      uid = parseInt(m[1]); gid = parseInt(m[2]);
    } else {
      const r = paths.find((p) => p.path === root);
      if (!r) return;
      uid = r.default_uid; gid = r.default_gid;
    }
    const recursive = e.is_dir && confirm('Recursive?');
    try {
      const res = await api<{ changed_count: number }>(`/api/nodes/${encodeURIComponent(nodeID)}/files/chown`, {
        method: 'POST', body: { path: fullPath(e.name), uid, gid, recursive },
      });
      alert(`Changed ${res.changed_count} entries.`);
      loadEntries();
    } catch (er) { alert('chown failed: ' + String(er)); }
  };

  return (
    <div className="mx-auto max-w-7xl px-6 py-8">
      <header className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Files</h1>
          <p className="text-xs text-zinc-500">Per-node browser. Operations bounded by configured managed roots.</p>
        </div>
        <div className="flex gap-2 text-xs">
          <button onClick={loadEntries} className="btn-ghost"><RefreshCw className="h-3.5 w-3.5" /> refresh</button>
          {root && <button onClick={onMkdir} className="btn-ghost"><FolderPlus className="h-3.5 w-3.5" /> new folder</button>}
          <label className="ml-2 flex cursor-pointer items-center gap-1 text-zinc-500">
            <input type="checkbox" checked={showHidden} onChange={(e) => setShowHidden(e.target.checked)} /> hidden
          </label>
        </div>
      </header>

      <div className="grid grid-cols-12 gap-4">
        <aside className="col-span-12 md:col-span-3">
          <div className="card p-3">
            <div className="mb-1 text-[11px] uppercase tracking-wide text-zinc-500">Nodes</div>
            <ul className="space-y-1">
              {nodes.map((n) => (
                <li key={n.id}>
                  <Link href={`/files?node=${encodeURIComponent(n.id)}`}
                    className={`block rounded-md px-2 py-1 text-sm ${n.id === nodeID ? 'bg-zinc-100 dark:bg-zinc-800' : 'hover:bg-zinc-100 dark:hover:bg-zinc-900'}`}
                  >{n.hostname}</Link>
                </li>
              ))}
            </ul>
          </div>
          {nodeID && (
            <div className="card mt-3 p-3">
              <div className="mb-1 text-[11px] uppercase tracking-wide text-zinc-500">Managed roots</div>
              <ul className="space-y-1">
                {paths.length === 0 && <li className="text-xs text-zinc-500">none yet</li>}
                {paths.map((p) => (
                  <li key={p.id}>
                    <Link href={`/files?node=${encodeURIComponent(nodeID)}&root=${encodeURIComponent(p.path)}`}
                      className={`block rounded-md px-2 py-1 text-sm ${p.path === root ? 'bg-zinc-100 dark:bg-zinc-800' : 'hover:bg-zinc-100 dark:hover:bg-zinc-900'}`}
                    >
                      <div>{p.label}</div>
                      <div className="font-mono text-[11px] text-zinc-500">{p.path}</div>
                      <div className="text-[10px] text-zinc-400">default {p.default_uid}:{p.default_gid}{p.read_only && ' · read-only'}</div>
                    </Link>
                  </li>
                ))}
              </ul>
              <AddRootForm nodeID={nodeID} onAdded={async () => {
                const r = await api<{ paths: NodePath[] }>(`/api/nodes/${encodeURIComponent(nodeID)}/paths`);
                setPaths(r.paths ?? []);
              }} />
            </div>
          )}
        </aside>

        <section className="col-span-12 md:col-span-9">
          <div className="card overflow-hidden p-0">
            <div className="flex items-center gap-1 border-b border-zinc-200 px-4 py-2 font-mono text-[11px] text-zinc-500 dark:border-zinc-800">
              <span>{root || '— pick a root —'}</span>
              {sub.split('/').filter(Boolean).map((seg, i) => (
                <span key={i} className="flex items-center gap-1">
                  <ChevronRight className="h-3 w-3" />
                  <span className="text-zinc-700 dark:text-zinc-300">{seg}</span>
                </span>
              ))}
              {root && <span className="ml-auto text-zinc-400">{entries.length} entries</span>}
            </div>

            {err && <div className="px-4 py-3 text-sm text-red-600">{err}</div>}
            {busy && <div className="px-4 py-3 text-xs text-zinc-500">Loading…</div>}

            {root ? (
              <table className="w-full text-sm">
                <thead className="border-b border-zinc-200 text-left text-[11px] uppercase tracking-wide text-zinc-500 dark:border-zinc-800">
                  <tr>
                    <th className="px-4 py-2">Name</th>
                    <th className="px-4 py-2">Size</th>
                    <th className="px-4 py-2">Owner</th>
                    <th className="px-4 py-2">Modified</th>
                    <th className="px-4 py-2"></th>
                  </tr>
                </thead>
                <tbody>
                  {sub && (
                    <tr className="cursor-pointer border-b border-zinc-100 hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-900" onClick={goUp}>
                      <td colSpan={5} className="px-4 py-2 font-mono text-zinc-500">..</td>
                    </tr>
                  )}
                  {entries.map((e) => (
                    <tr key={e.name} className="border-b border-zinc-100 last:border-0 hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-900">
                      <td className="px-4 py-2" onClick={() => goTo(e.name, e.is_dir)} role={e.is_dir ? 'button' : undefined}>
                        <span className="inline-flex items-center gap-2">
                          {e.is_dir ? <Folder className="h-4 w-4 text-amber-500" /> : <FileIcon className="h-4 w-4 text-zinc-500" />}
                          <span className={e.is_dir ? 'cursor-pointer font-medium hover:underline' : ''}>{e.name}</span>
                        </span>
                      </td>
                      <td className="px-4 py-2 font-mono text-[12px] text-zinc-500">{e.is_dir ? '—' : fmtBytes(e.size)}</td>
                      <td className="px-4 py-2 font-mono text-[12px] text-zinc-500">{e.owner_name || e.uid}:{e.gid}</td>
                      <td className="px-4 py-2 text-[11px] text-zinc-500">{fmtAbsTime(e.mtime_ms)}</td>
                      <td className="px-4 py-2 text-right">
                        <button onClick={() => onChown(e)} className="btn-ghost" title="chown"><UserCog className="h-3.5 w-3.5" /></button>
                        <button onClick={() => onDelete(e)} className="btn-ghost text-red-600" title="delete"><Trash2 className="h-3.5 w-3.5" /></button>
                      </td>
                    </tr>
                  ))}
                  {!busy && entries.length === 0 && (
                    <tr><td colSpan={5} className="px-4 py-6 text-center text-zinc-500">Empty.</td></tr>
                  )}
                </tbody>
              </table>
            ) : (
              <div className="p-8 text-center text-sm text-zinc-500">
                {nodeID ? 'Pick a managed root from the sidebar.' : 'Pick a node from the sidebar.'}
              </div>
            )}
          </div>
        </section>
      </div>
    </div>
  );
}

function AddRootForm({ nodeID, onAdded }: { nodeID: string; onAdded: () => void }) {
  const [open, setOpen] = useState(false);
  const [label, setLabel] = useState('');
  const [path, setPath] = useState('');
  const [uid, setUID] = useState(1000);
  const [gid, setGID] = useState(1000);

  if (!open) {
    return (
      <button onClick={() => setOpen(true)} className="mt-3 w-full rounded-md border border-dashed border-zinc-300 px-2 py-1 text-xs text-zinc-500 hover:bg-zinc-50 dark:border-zinc-700 dark:hover:bg-zinc-900">
        + add root
      </button>
    );
  }
  return (
    <div className="mt-3 space-y-2 rounded-md border border-zinc-200 p-2 text-xs dark:border-zinc-800">
      <input placeholder="label" value={label} onChange={(e) => setLabel(e.target.value)} className="input h-8 text-xs" />
      <input placeholder="/abs/path" value={path} onChange={(e) => setPath(e.target.value)} className="input h-8 font-mono text-xs" />
      <div className="flex gap-2">
        <input type="number" value={uid} onChange={(e) => setUID(+e.target.value)} className="input h-8 text-xs" placeholder="uid" />
        <input type="number" value={gid} onChange={(e) => setGID(+e.target.value)} className="input h-8 text-xs" placeholder="gid" />
      </div>
      <div className="flex justify-end gap-1">
        <button onClick={() => setOpen(false)} className="btn-ghost">cancel</button>
        <button
          onClick={async () => {
            try {
              await api(`/api/nodes/${encodeURIComponent(nodeID)}/paths`, {
                method: 'POST', body: { label, path, default_uid: uid, default_gid: gid },
              });
              setOpen(false); setLabel(''); setPath('');
              onAdded();
            } catch (e) { alert('add failed: ' + String(e)); }
          }}
          className="btn-primary"
        >save</button>
      </div>
    </div>
  );
}
