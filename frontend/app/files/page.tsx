'use client';

import { Suspense, useCallback, useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { api, errText } from '@/lib/api';
import type { NodeView } from '@/lib/types';
import { AuthShell, useMe } from '@/components/auth-shell';
import { fmtBytes, fmtAbsTime } from '@/lib/format';
import { Folder, File as FileIcon, ChevronRight, RefreshCw, HardDrive, Lock, Server } from 'lucide-react';
import { DelayedSpinner, EmptyState, ErrorState, TableSkeleton } from '@/components/feedback';
import { Field, inputCls } from '@/components/field';
import { toast } from '@/components/toast';

interface NodePath {
  id: number;
  node_id: string;
  label: string;
  path: string;
  created_at: number;
}

interface FileEntry {
  name: string;
  is_dir: boolean;
  size: number;
  mtime_ms: number;
  mode: number;
  // proto3 JSON omits zero values, so a root-owned entry (uid 0) arrives
  // without these fields at all. Absent means 0.
  uid?: number;
  gid?: number;
  owner_name?: string;
  group_name?: string;
  symlink_target: string;
}

export default function FilesPage() {
  return (
    <AuthShell>
      <Suspense fallback={<div className="page"><TableSkeleton /></div>}>
        <FilesGate />
      </Suspense>
    </AuthShell>
  );
}

function FilesGate() {
  const me = useMe();
  if (me.filebrowser) return <Files />;
  return (
    <div className="page">
      <h1 className="page-title">Files</h1>
      <div className="card mt-6 flex max-w-2xl items-start gap-3">
        <Lock className="mt-0.5 h-5 w-5 shrink-0 text-zinc-400" aria-hidden />
        <div>
          <h2 className="text-sm font-semibold">The file browser is disabled</h2>
          <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
            It reads the host filesystem of every node, so it is off unless the leader runs with{' '}
            <code className="rounded bg-zinc-100 px-1 py-0.5 text-xs dark:bg-zinc-800">BPM_FILEBROWSER=true</code>.
            Set that on the leader service and redeploy to turn it on. It cannot be enabled from here.
          </p>
        </div>
      </div>
    </div>
  );
}

// VIRTUAL_ROOT is the implicit "/" entry every node gets in the sidebar.
// Backend's findManagedRoot accepts any absolute path as virtual; here we
// just synthesize the sidebar item so the user has a one-click entry into
// the full filesystem without configuring a Path Profile.
const VIRTUAL_ROOT: NodePath = {
  id: -1,
  node_id: '',
  label: 'Full filesystem (/)',
  path: '/',
  created_at: 0,
};

function Files() {
  const params = useSearchParams();
  const nodeID = params.get('node') ?? '';
  const nodeIDRef = useRef(nodeID);
  nodeIDRef.current = nodeID;
  const root = params.get('root') || '/';   // implicit virtual root when empty
  const sub = params.get('sub') ?? '';

  const [nodes, setNodes] = useState<NodeView[]>([]);
  const [paths, setPaths] = useState<NodePath[]>([]);
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [showHidden, setShowHidden] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [nodesLoaded, setNodesLoaded] = useState(false);
  const [nodesErr, setNodesErr] = useState<string | null>(null);

  const loadNodes = useCallback(async () => {
    try {
      const r = await api<{ nodes: NodeView[] }>('/api/nodes');
      setNodes(r.nodes ?? []);
      setNodesErr(null);
    } catch (e) {
      setNodesErr(errText(e, 'Could not load nodes.'));
    } finally {
      setNodesLoaded(true);
    }
  }, []);
  useEffect(() => { loadNodes(); }, [loadNodes]);

  useEffect(() => {
    // The previous node's shortcuts must not linger (or win a race) here.
    setPaths([]);
    if (!nodeID) return;
    let alive = true;
    (async () => {
      try {
        const r = await api<{ paths: NodePath[] }>(`/api/nodes/${encodeURIComponent(nodeID)}/paths`);
        if (alive) setPaths(r.paths ?? []);
      } catch { /* */ }
    })();
    return () => { alive = false; };
  }, [nodeID]);

  // Only the newest listing may write: switching node or folder while a slow
  // listing runs must not show the old directory.
  const entriesSeq = useRef(0);
  const loadEntries = useCallback(async () => {
    const my = ++entriesSeq.current;
    if (!nodeID || !root) { setEntries([]); setBusy(false); return; }
    setBusy(true);
    setErr(null);
    try {
      const r = await api<{ entries: FileEntry[] }>(
        `/api/nodes/${encodeURIComponent(nodeID)}/files?root=${encodeURIComponent(root)}&sub=${encodeURIComponent(sub)}&show_hidden=${showHidden}`
      );
      if (my !== entriesSeq.current) return;
      setEntries(r.entries ?? []);
    } catch (e: unknown) {
      if (my !== entriesSeq.current) return;
      setErr(errText(e, 'Could not read this directory.'));
      setEntries([]);
    } finally {
      if (my === entriesSeq.current) setBusy(false);
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

  return (
    <div className="page">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Files</h1>
          <p className="page-sub">
            Read-only per-node browser. Full filesystem by default, optional Path Profiles as shortcuts.
          </p>
        </div>
        <div className="flex items-center gap-3 text-sm">
          <label className="flex cursor-pointer items-center gap-2 text-zinc-700 dark:text-zinc-300">
            <input type="checkbox" checked={showHidden} onChange={(e) => setShowHidden(e.target.checked)} /> Show hidden files
          </label>
          <button onClick={loadEntries} disabled={!nodeID || busy} className="btn-ghost"><RefreshCw className="h-4 w-4" /> Refresh</button>
        </div>
      </header>

      {nodesErr && <ErrorState message={nodesErr} onRetry={loadNodes} />}

      <div className="grid grid-cols-12 gap-4">
        <aside className="col-span-12 md:col-span-4 lg:col-span-3 3xl:col-span-2">
          <div className="card p-3">
            <div className="section-title mb-2 px-2">Nodes</div>
            {!nodesLoaded && <div className="space-y-2 px-2 py-1">{[0, 1, 2].map((i) => <div key={i} className="skeleton h-4" />)}</div>}
            {nodesLoaded && nodes.length === 0 && !nodesErr && <p className="px-2 py-1 text-xs text-zinc-500">No nodes connected.</p>}
            <ul className="space-y-1">
              {nodes.map((n) => (
                <li key={n.id}>
                  <Link href={`/files?node=${encodeURIComponent(n.id)}`}
                    aria-current={n.id === nodeID ? 'page' : undefined}
                    className={`${NAV_LINK} ${n.id === nodeID ? 'bg-zinc-100 font-medium dark:bg-zinc-800' : ''}`}
                  >{n.hostname}</Link>
                </li>
              ))}
            </ul>
          </div>
          {nodeID && (
            <div className="card mt-3 p-3">
              <div className="section-title mb-2 px-2">Roots</div>
              <ul className="space-y-1">
                <li>
                  <Link href={`/files?node=${encodeURIComponent(nodeID)}&root=${encodeURIComponent('/')}`}
                    className={`${NAV_LINK} ${root === '/' ? 'bg-zinc-100 font-medium dark:bg-zinc-800' : ''}`}
                  >
                    <div className="inline-flex items-center gap-1.5">
                      <HardDrive className="h-3.5 w-3.5 text-zinc-500" />
                      <span>{VIRTUAL_ROOT.label}</span>
                    </div>
                  </Link>
                </li>
              </ul>

              <div className="section-title mb-2 mt-4 px-2">Path Profiles</div>
              <ul className="space-y-1">
                {paths.length === 0 && <li className="px-2 text-xs text-zinc-500">None yet. Add one as a shortcut to a directory.</li>}
                {paths.map((p) => (
                  <li key={p.id}>
                    <Link href={`/files?node=${encodeURIComponent(nodeID)}&root=${encodeURIComponent(p.path)}`}
                      className={`${NAV_LINK} ${p.path === root ? 'bg-zinc-100 font-medium dark:bg-zinc-800' : ''}`}
                    >
                      <div>{p.label}</div>
                      <div className="font-mono text-2xs text-zinc-500">{p.path}</div>
                    </Link>
                  </li>
                ))}
              </ul>
              <AddRootForm nodeID={nodeID} onAdded={async () => {
                const forNode = nodeID;
                try {
                  const r = await api<{ paths: NodePath[] }>(`/api/nodes/${encodeURIComponent(forNode)}/paths`);
                  if (forNode === nodeIDRef.current) setPaths(r.paths ?? []);
                } catch { /* the new root shows after the next reload */ }
              }} />
            </div>
          )}
        </aside>

        <section className="col-span-12 min-w-0 md:col-span-8 lg:col-span-9 3xl:col-span-10">
          <div className="card overflow-hidden p-0">
            <div className="flex items-center gap-1 border-b border-zinc-200 px-4 py-2.5 font-mono text-xs text-zinc-500 dark:border-zinc-800">
              <span>{root || 'pick a root'}</span>
              {sub.split('/').filter(Boolean).map((seg, i) => (
                <span key={i} className="flex items-center gap-1">
                  <ChevronRight className="h-3 w-3" />
                  <span className="text-zinc-700 dark:text-zinc-300">{seg}</span>
                </span>
              ))}
              <span className="ml-auto inline-flex items-center gap-2 text-zinc-400"><DelayedSpinner active={busy} />{nodeID && `${entries.length} entries`}</span>
            </div>

            {err && <div className="px-4 pt-4"><ErrorState message={err} onRetry={loadEntries} /></div>}

            {nodeID ? (
              <div className="overflow-x-auto">
              <table className="w-full min-w-[40rem] text-sm">
                <thead className="border-b border-zinc-200 dark:border-zinc-800">
                  <tr>
                    <th className="th">Name</th>
                    <th className="th">Size</th>
                    <th className="th">Owner</th>
                    <th className="th">Modified</th>
                  </tr>
                </thead>
                <tbody>
                  {sub && (
                    <tr className="border-b border-zinc-100 dark:border-zinc-800">
                      <td colSpan={4} className="p-0">
                        <button onClick={goUp} className="w-full px-4 py-2.5 text-left font-mono text-zinc-500 transition hover:bg-zinc-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent focus-visible:ring-offset-0 dark:hover:bg-zinc-800/50">
                          .. <span className="sr-only">up one level</span>
                        </button>
                      </td>
                    </tr>
                  )}
                  {entries.map((e) => (
                    <tr key={e.name} className="border-b border-zinc-100 last:border-0 hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-800/50">
                      <td className="td">
                        {e.is_dir ? (
                          <button onClick={() => goTo(e.name, true)} className="inline-flex items-center gap-2 rounded font-medium hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-accent">
                            <Folder className="h-4 w-4 text-amber-500" aria-hidden />{e.name}
                          </button>
                        ) : (
                          <span className="inline-flex items-center gap-2">
                            <FileIcon className="h-4 w-4 text-zinc-500" aria-hidden />{e.name}
                          </span>
                        )}
                      </td>
                      <td className="td font-mono text-xs text-zinc-500">{e.is_dir ? '-' : fmtBytes(e.size)}</td>
                      <td className="td font-mono text-xs text-zinc-500">{e.owner_name || (e.uid ?? 0)}:{e.group_name || (e.gid ?? 0)}</td>
                      <td className="td text-2xs text-zinc-500">{fmtAbsTime(e.mtime_ms)}</td>
                    </tr>
                  ))}
                  {!busy && !err && entries.length === 0 && (
                    <tr><td colSpan={4}><EmptyState title="This directory is empty">{showHidden ? undefined : 'Hidden files are not shown. Turn on Show hidden files to include them.'}</EmptyState></td></tr>
                  )}
                </tbody>
              </table>
              </div>
            ) : (
              <EmptyState className="min-h-[40vh] justify-center" icon={<Server className="h-6 w-6" />} title="Pick a node">
                Choose a node on the left to browse its filesystem.
              </EmptyState>
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
  const [errs, setErrs] = useState<Record<string, string>>({});
  const [formErr, setFormErr] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  if (!open) {
    return (
      <button onClick={() => setOpen(true)} className="mt-4 w-full rounded-md border border-dashed border-zinc-300 px-2 py-2 text-xs font-medium text-zinc-700 transition hover:border-zinc-400 hover:bg-zinc-50 active:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800/50">
        Add path profile
      </button>
    );
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const next: Record<string, string> = {};
    if (!label.trim()) next.label = 'Give it a label.';
    if (!path.startsWith('/')) next.path = 'Use an absolute path, starting with /.';
    setErrs(next);
    if (Object.keys(next).length) return;
    setSaving(true);
    setFormErr(null);
    try {
      await api(`/api/nodes/${encodeURIComponent(nodeID)}/paths`, {
        method: 'POST', body: { label: label.trim(), path },
      });
      toast.success(`Path profile ${label.trim()} added.`);
      setOpen(false); setLabel(''); setPath('');
      onAdded();
    } catch (err) {
      setFormErr(errText(err, 'Adding the path profile failed.'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <form noValidate onSubmit={submit} className="mt-4 space-y-3 rounded-md border border-zinc-200 p-3 text-xs dark:border-zinc-800">
      <Field label="Label" error={errs.label}>
        <input placeholder="App data" value={label} onChange={(e) => setLabel(e.target.value)} className={inputCls(errs.label, 'h-8 text-xs')} />
      </Field>
      <Field label="Path" error={errs.path}>
        <input placeholder="/srv/app" value={path} onChange={(e) => setPath(e.target.value)} className={inputCls(errs.path, 'h-8 font-mono text-xs')} />
      </Field>
      {formErr && <p role="alert" className="field-error">{formErr}</p>}
      <div className="flex justify-end gap-1">
        <button type="button" onClick={() => { setOpen(false); setErrs({}); setFormErr(null); }} disabled={saving} className="btn-ghost btn-sm">Cancel</button>
        <button type="submit" disabled={saving} className="btn-primary btn-sm">{saving ? 'Saving…' : 'Save'}</button>
      </div>
    </form>
  );
}

const NAV_LINK =
  'block rounded-md px-2 py-1.5 text-sm transition hover:bg-zinc-100 active:bg-zinc-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent focus-visible:ring-offset-0 dark:hover:bg-zinc-800 dark:active:bg-zinc-700';
