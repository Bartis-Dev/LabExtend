'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { UserCog } from 'lucide-react';
import { api } from '@/lib/api';
import { Modal } from '@/components/modal';

export interface ChownTarget {
  name: string;
  is_dir: boolean;
  // Optional on the wire: the agent's response is proto3 JSON, which omits
  // zero values — so a root-owned file (uid 0, gid 0) arrives with no uid and
  // no gid at all. Absent means 0 here, never "unknown".
  uid?: number;
  gid?: number;
  owner_name?: string;
  group_name?: string;
}

interface Preset {
  key: string;
  label: string;
  hint: string;
  uid: number;
  gid: number;
}

/** Resolution state of one owner field: a number, a resolved name, or an error. */
type Resolved =
  | { kind: 'empty' }
  | { kind: 'numeric'; id: number }
  | { kind: 'looking' }
  | { kind: 'name'; id: number; name: string }
  | { kind: 'error'; message: string };

/**
 * ChownModal replaces the window.prompt("uid:gid") + confirm("Recursive?")
 * pair. Beyond looking like the rest of the app, it lets you type a user *name*
 * (resolved against the node), shows what the file has now, and reports the
 * changed count inline instead of through an alert box.
 */
export function ChownModal({ nodeID, path, file, defaultOwner, onClose, onDone }: {
  nodeID: string;
  path: string;
  file: ChownTarget;
  /** Owner configured on the enclosing path profile, if there is one. */
  defaultOwner?: { uid: number; gid: number; label?: string } | null;
  onClose: () => void;
  onDone: (changed: number) => void;
}) {
  const curUID = file.uid ?? 0;
  const curGID = file.gid ?? 0;
  const [userInput, setUserInput] = useState(String(curUID));
  const [groupInput, setGroupInput] = useState(String(curGID));
  const [recursive, setRecursive] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const user = useResolvedOwner(nodeID, userInput, 'user');
  const group = useResolvedOwner(nodeID, groupInput, 'group');

  const presets = useMemo<Preset[]>(() => {
    const out: Preset[] = [{ key: 'root', label: 'root:root', hint: 'uid 0 · gid 0', uid: 0, gid: 0 }];
    if (defaultOwner) {
      out.unshift({
        key: 'profile',
        label: defaultOwner.label || `${defaultOwner.uid}:${defaultOwner.gid}`,
        hint: 'path profile default',
        uid: defaultOwner.uid,
        gid: defaultOwner.gid,
      });
    }
    return out;
  }, [defaultOwner]);

  const uid = user.kind === 'numeric' ? user.id : user.kind === 'name' ? user.id : null;
  const gid = group.kind === 'numeric' ? group.id : group.kind === 'name' ? group.id : null;
  const ready = uid !== null && gid !== null && !busy;
  const unchanged = uid === curUID && gid === curGID;

  const apply = async () => {
    if (uid === null || gid === null) return;
    setBusy(true);
    setErr(null);
    try {
      const res = await api<{ changed_count: number }>(
        `/api/nodes/${encodeURIComponent(nodeID)}/files/chown`,
        { method: 'POST', body: { path, uid, gid, recursive } },
      );
      onDone(res.changed_count);
    } catch (e: unknown) {
      setErr((e as { body?: { error?: string } })?.body?.error ?? String(e));
      setBusy(false);
    }
  };

  return (
    <Modal
      title="Change owner"
      subtitle={<span className="break-all font-mono text-[11px]">{path}</span>}
      icon={<UserCog className="h-4 w-4 text-accent" />}
      onClose={onClose}
      footer={
        <>
          <button onClick={onClose} className="btn-ghost">Cancel</button>
          <button onClick={apply} disabled={!ready} className="btn-primary">
            {busy ? 'Applying…' : unchanged ? 'Owner unchanged' : `Set ${uid ?? '?'}:${gid ?? '?'}${recursive ? ' recursively' : ''}`}
          </button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="flex items-center justify-between rounded-md bg-zinc-50 px-3 py-2 text-xs dark:bg-zinc-900/50">
          <span className="text-zinc-500">Currently</span>
          <span className="font-mono">
            {file.owner_name || curUID}:{file.group_name || curGID}
            <span className="ml-1.5 text-zinc-400">({curUID}:{curGID})</span>
          </span>
        </div>

        {presets.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {presets.map((p) => (
              <button
                key={p.key}
                onClick={() => { setUserInput(String(p.uid)); setGroupInput(String(p.gid)); }}
                className="rounded-md border border-zinc-200 px-2.5 py-1.5 text-left text-xs transition hover:border-accent hover:bg-accent/5 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:border-zinc-700 dark:hover:border-accent"
              >
                <span className="font-mono">{p.label}</span>
                <span className="ml-1.5 text-[10px] text-zinc-500">{p.hint}</span>
              </button>
            ))}
          </div>
        )}

        <div className="grid grid-cols-2 gap-3">
          <OwnerField label="User" placeholder="name or uid" value={userInput} onChange={setUserInput} state={user} />
          <OwnerField label="Group" placeholder="gid" value={groupInput} onChange={setGroupInput} state={group} />
        </div>

        {file.is_dir && (
          <label className="flex cursor-pointer items-start gap-2 rounded-md border border-zinc-200 px-3 py-2 text-xs transition hover:bg-zinc-50 dark:border-zinc-700 dark:hover:bg-zinc-800/50">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={recursive}
              onChange={(e) => setRecursive(e.target.checked)}
            />
            <span>
              <span className="font-medium">Apply recursively</span>
              <span className="mt-0.5 block text-zinc-500">
                Every file and subdirectory inside. There is no undo — a wrong owner on a data volume can stop the
                service that owns it.
              </span>
            </span>
          </label>
        )}

        {err && (
          <p className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
            {err}
          </p>
        )}
      </div>
    </Modal>
  );
}

function OwnerField({ label, placeholder, value, onChange, state }: {
  label: string;
  placeholder: string;
  value: string;
  onChange: (v: string) => void;
  state: Resolved;
}) {
  return (
    <label className="block">
      <span className="text-xs text-zinc-500">{label}</span>
      <input
        className="input mt-1 font-mono"
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        spellCheck={false}
        autoComplete="off"
      />
      <span className="mt-1 block h-4 text-[11px]">
        {state.kind === 'looking' && <span className="text-zinc-400">resolving…</span>}
        {state.kind === 'name' && <span className="text-emerald-600 dark:text-emerald-400">→ {state.id} ({state.name})</span>}
        {state.kind === 'error' && <span className="text-red-600 dark:text-red-400">{state.message}</span>}
        {state.kind === 'empty' && <span className="text-red-600 dark:text-red-400">required</span>}
      </span>
    </label>
  );
}

/**
 * useResolvedOwner turns free text into a numeric id. Digits are taken as-is;
 * anything else is looked up on the node. Lookups are debounced and stale
 * responses are dropped, so typing "www-data" does not leave the field showing
 * the answer for "www-dat".
 */
function useResolvedOwner(nodeID: string, input: string, kind: 'user' | 'group'): Resolved {
  const [state, setState] = useState<Resolved>({ kind: 'empty' });
  const seq = useRef(0);

  useEffect(() => {
    const raw = input.trim();
    const mine = ++seq.current;

    if (raw === '') {
      setState({ kind: 'empty' });
      return;
    }
    if (/^\d+$/.test(raw)) {
      setState({ kind: 'numeric', id: Number(raw) });
      return;
    }
    if (kind === 'group') {
      // The agent only exposes user lookup; a group has to be given numerically.
      setState({ kind: 'error', message: 'group must be numeric' });
      return;
    }

    setState({ kind: 'looking' });
    const t = setTimeout(async () => {
      try {
        const r = await api<{ name: string; uid: number; gid: number }>(
          `/api/nodes/${encodeURIComponent(nodeID)}/files/lookup-user?name=${encodeURIComponent(raw)}`,
        );
        if (seq.current !== mine) return; // a newer keystroke already won
        setState({ kind: 'name', id: r.uid, name: r.name });
      } catch {
        if (seq.current !== mine) return;
        setState({ kind: 'error', message: 'no such user on this node' });
      }
    }, 300);
    return () => clearTimeout(t);
  }, [nodeID, input, kind]);

  return state;
}
