'use client';

import { useCallback, useEffect, useState } from 'react';
import { Webhook } from 'lucide-react';
import { api, errText } from '@/lib/api';
import type { EventType, WebhookConfig } from '@/lib/types';
import { AuthShell } from '@/components/auth-shell';
import { Modal } from '@/components/modal';
import { toast } from '@/components/toast';
import { confirmDialog } from '@/components/confirm';
import { Field, inputCls } from '@/components/field';
import { EmptyState, ErrorState, TableSkeleton } from '@/components/feedback';

export default function WebhooksPage() {
  return (
    <AuthShell>
      <Webhooks />
    </AuthShell>
  );
}

const EVENTS: { type: EventType; label: string; hint: string }[] = [
  { type: 'backup.failed', label: 'Backup failed', hint: 'A run ends failed or partial' },
  { type: 'backup.retention_failed', label: 'Backup retention failed', hint: 'Old archives could not be deleted' },
  { type: 'node.offline', label: 'Node offline', hint: 'No heartbeat for 60s' },
  { type: 'node.online', label: 'Node back online', hint: 'Connected again for 30s' },
  { type: 'container.crashloop', label: 'Container crash loop', hint: '3 or more restarts within 60s' },
];
const ALL_EVENTS = EVENTS.map((e) => e.type);

const DISCORD_HOSTS = ['discord.com', 'discordapp.com', 'ptb.discord.com', 'canary.discord.com'];

// webhookURLError mirrors the leader's discord.ValidWebhookURL, so a bad URL
// is caught in the form instead of as a 400 after Save.
function webhookURLError(raw: string): string | null {
  let u: URL;
  try { u = new URL(raw.trim()); } catch { return 'This is not a valid URL.'; }
  if (u.protocol !== 'https:') return 'The URL must start with https://.';
  if (!DISCORD_HOSTS.includes(u.host)) return 'The URL must point to discord.com (or discordapp.com, ptb.discord.com, canary.discord.com).';
  if (u.username || u.password || !u.pathname.startsWith('/api/webhooks/')) return 'Use a Discord webhook URL: https://discord.com/api/webhooks/...';
  return null;
}

function Webhooks() {
  const [items, setItems] = useState<WebhookConfig[]>([]);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [editing, setEditing] = useState<Partial<WebhookConfig> | null>(null);
  const [saveErr, setSaveErr] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [fieldErr, setFieldErr] = useState<Record<string, string>>({});
  // The masked URL the edited webhook came with; sending it back unchanged
  // keeps the stored URL, anything else is a new URL and gets validated.
  const [origURL, setOrigURL] = useState('');

  const load = async () => {
    try {
      const r = await api<{ webhooks: WebhookConfig[] }>('/api/webhooks');
      setItems(r.webhooks ?? []);
      setLoadErr(null);
    } catch (e) {
      setLoadErr(errText(e, 'Could not load webhooks.'));
    } finally {
      setLoaded(true);
    }
  };

  useEffect(() => { load(); }, []);

  const closeEdit = useCallback(() => { setEditing(null); setSaveErr(null); setFieldErr({}); }, []);

  const save = async (w: Partial<WebhookConfig>) => {
    const errs: Record<string, string> = {};
    if (!w.name?.trim()) errs.name = 'Give the webhook a name.';
    const url = w.url?.trim() ?? '';
    const urlChanged = !w.id || (url !== '' && url !== origURL);
    if (!w.id && !url) errs.url = 'Paste the webhook URL.';
    else if (urlChanged) {
      const ue = webhookURLError(url);
      if (ue) errs.url = ue;
    }
    setFieldErr(errs);
    if (Object.keys(errs).length) return;

    const payload = {
      name: w.name ?? '',
      kind: w.kind ?? 'discord',
      url: url,
      enabled: w.enabled ?? true,
      event_types: w.event_types ?? [],
    };
    setSaving(true);
    try {
      if (w.id) await api(`/api/webhooks/${w.id}`, { method: 'PUT', body: payload });
      else await api('/api/webhooks', { method: 'POST', body: payload });
      closeEdit();
      toast.success(`Webhook ${payload.name} saved.`);
      load();
    } catch (e) {
      setSaveErr(errText(e, 'Saving failed.'));
    } finally {
      setSaving(false);
    }
  };
  const remove = async (w: WebhookConfig) => {
    const ok = await confirmDialog({
      title: `Delete ${w.name}?`,
      body: 'Events stop going to this webhook. Backup plans that post their successful runs to it lose that target.',
      confirmLabel: 'Delete webhook',
      danger: true,
    });
    if (!ok) return;
    try {
      await api(`/api/webhooks/${w.id}`, { method: 'DELETE' });
      toast.success(`Deleted ${w.name}.`);
      load();
    } catch (e) {
      toast.error(errText(e, 'Delete failed.'));
    }
  };
  const test = async (w: WebhookConfig) => {
    setTesting(w.id);
    try {
      await api(`/api/webhooks/${w.id}/test`, { method: 'POST' });
      toast.success(`Test message sent to ${w.name}.`);
    } catch (e) {
      toast.error('Test failed: ' + errText(e, 'no response'));
    } finally {
      setTesting(null);
    }
  };

  const newWebhook = () => { setFieldErr({}); setSaveErr(null); setOrigURL(''); setEditing({ enabled: true, kind: 'discord', event_types: ALL_EVENTS }); };

  const toggleEvent = (t: EventType) => {
    if (!editing) return;
    const cur = editing.event_types ?? [];
    setEditing({ ...editing, event_types: cur.includes(t) ? cur.filter((x) => x !== t) : [...cur, t] });
  };

  return (
    <div className="page">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Webhooks</h1>
          <p className="page-sub">Each event goes to every enabled webhook subscribed to it.</p>
        </div>
        <button onClick={newWebhook} className="btn-primary">New webhook</button>
      </header>

      {loadErr && <ErrorState message={loadErr} onRetry={load} />}

      {!loaded ? <TableSkeleton rows={3} cols={4} /> : (
      <div className="card overflow-x-auto p-0">
        <table className="w-full min-w-[48rem] text-sm">
          <thead className="border-b border-zinc-200 dark:border-zinc-800">
            <tr>
              <th className="th">Name</th>
              <th className="th">URL</th>
              <th className="th">Events</th>
              <th className="th">Status</th>
              <th className="th"><span className="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {items.map((w) => (
              <tr key={w.id} className="border-b border-zinc-100 last:border-0 dark:border-zinc-800">
                <td className="td font-medium">{w.name}<div className="text-2xs font-normal text-zinc-500">{w.kind}</div></td>
                <td className="td font-mono text-2xs text-zinc-500">{w.url}</td>
                <td className="td">
                  <div className="flex flex-wrap gap-1">
                    {(w.event_types ?? []).length === 0
                      ? <span className="text-xs text-zinc-500">none</span>
                      : w.event_types.map((t) => <span key={t} className="badge-outline">{t}</span>)}
                  </div>
                </td>
                <td className="td"><span className={w.enabled ? 'badge-green' : 'badge-zinc'}>{w.enabled ? 'enabled' : 'disabled'}</span></td>
                <td className="td whitespace-nowrap text-right">
                  <button onClick={() => test(w)} disabled={testing === w.id} className="btn-ghost btn-sm">{testing === w.id ? 'Sending…' : 'Send test'}</button>
                  <button onClick={() => { setFieldErr({}); setSaveErr(null); setOrigURL(w.url ?? ''); setEditing(w); }} className="btn-ghost btn-sm">Edit</button>
                  <button onClick={() => remove(w)} className="btn-ghost-danger btn-sm">Delete</button>
                </td>
              </tr>
            ))}
            {items.length === 0 && !loadErr && (
              <tr><td colSpan={5}>
                <EmptyState
                  icon={<Webhook className="h-6 w-6" />}
                  title="No webhooks yet"
                  action={<button onClick={newWebhook} className="btn-primary">New webhook</button>}
                >
                  Add a Discord webhook to get failed backups, offline nodes and crash loops in a channel.
                </EmptyState>
              </td></tr>
            )}
          </tbody>
        </table>
      </div>
      )}

      {editing && (
        <Modal
          title={editing.id ? 'Edit webhook' : 'New webhook'}
          onClose={closeEdit}
          dismissable={!saving}
          footer={
            <>
              <button type="button" onClick={closeEdit} disabled={saving} className="btn-ghost">Cancel</button>
              <button type="submit" form="webhook-form" disabled={saving} className="btn-primary">{saving ? 'Saving…' : 'Save'}</button>
            </>
          }
        >
          <form id="webhook-form" noValidate onSubmit={(e) => { e.preventDefault(); save(editing); }} className="space-y-4">
            <Field label="Name" error={fieldErr.name}>
              <input className={inputCls(fieldErr.name)} value={editing.name ?? ''} onChange={(e) => setEditing({ ...editing, name: e.target.value })} />
            </Field>
            <Field label="Kind">
              <select className="input" value={editing.kind ?? 'discord'} onChange={(e) => setEditing({ ...editing, kind: e.target.value })}>
                <option value="discord">discord</option>
              </select>
            </Field>
            <Field label={<>URL {editing.id && '(leave masked to keep)'}</>} error={fieldErr.url}>
              <input className={inputCls(fieldErr.url, 'font-mono text-xs')} placeholder="https://discord.com/api/webhooks/…/…" value={editing.url ?? ''} onChange={(e) => {
                setEditing({ ...editing, url: e.target.value });
                // Clear a stale error as soon as the URL is fixed.
                if (fieldErr.url && !webhookURLError(e.target.value)) setFieldErr((f) => ({ ...f, url: '' }));
              }} />
            </Field>
            <fieldset>
              <legend className="label">Events</legend>
              <div className="mt-1.5 divide-y divide-zinc-100 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-700">
                {EVENTS.map((ev) => (
                  <label key={ev.type} className="flex cursor-pointer items-start gap-2.5 px-3 py-2 hover:bg-zinc-50 dark:hover:bg-zinc-800/60">
                    <input
                      type="checkbox"
                      className="mt-0.5"
                      checked={(editing.event_types ?? []).includes(ev.type)}
                      onChange={() => toggleEvent(ev.type)}
                    />
                    <span>
                      <span className="block text-sm">{ev.label}</span>
                      <span className="block text-2xs text-zinc-500">{ev.hint}</span>
                    </span>
                  </label>
                ))}
              </div>
            </fieldset>
            <label className="flex cursor-pointer items-center gap-2 text-sm">
              <input type="checkbox" checked={editing.enabled ?? true} onChange={(e) => setEditing({ ...editing, enabled: e.target.checked })} />
              Enabled
            </label>
            {saveErr && <p role="alert" className="alert-error">{saveErr}</p>}
          </form>
        </Modal>
      )}

    </div>
  );
}
