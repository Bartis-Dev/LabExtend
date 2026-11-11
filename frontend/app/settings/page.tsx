'use client';

import { useEffect, useState } from 'react';
import clsx from 'clsx';
import { api, errText } from '@/lib/api';
import { AuthShell, useMe } from '@/components/auth-shell';
import { toast } from '@/components/toast';
import { ErrorState } from '@/components/feedback';

type AuditMode = 'auto' | 'on' | 'off';

interface AuditState {
  mode: AuditMode;
  enabled: boolean;
  keep: number;
  users: number;
}

const MODES: { value: AuditMode; label: string; hint: string }[] = [
  { value: 'auto', label: 'Auto', hint: 'Full log with two or more users. With a single user only the newest 100 entries are kept.' },
  { value: 'on', label: 'On', hint: 'Keep every entry.' },
  { value: 'off', label: 'Off', hint: 'Record nothing. Existing entries stay until you switch back.' },
];

function effective(s: AuditState): string {
  const users = `${s.users} user${s.users === 1 ? '' : 's'}`;
  if (!s.enabled) return 'Off: nothing is recorded';
  if (s.mode === 'on') return 'On: keeping all entries';
  return s.keep > 0 ? `Auto: ${users}, keeping last ${s.keep} entries` : `Auto: ${users}, keeping all entries`;
}

export default function SettingsPage() {
  return <AuthShell><Settings /></AuthShell>;
}

function Settings() {
  const me = useMe();
  const [audit, setAudit] = useState<AuditState | null>(null);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [saving, setSaving] = useState<AuditMode | null>(null);

  const load = () => {
    setLoadErr(null);
    api<{ audit: AuditState }>('/api/settings')
      .then((r) => setAudit(r.audit))
      .catch((e) => setLoadErr(errText(e, 'Could not load settings.')));
  };
  useEffect(load, []);

  const choose = async (mode: AuditMode) => {
    if (!audit || mode === audit.mode || saving) return;
    setSaving(mode);
    try {
      const r = await api<{ audit: AuditState }>('/api/settings', { method: 'PUT', body: { audit_mode: mode } });
      setAudit(r.audit);
      toast.success('Audit log setting saved. Reload to update the sidebar.');
    } catch (e) {
      toast.error(errText(e, 'Saving failed.'));
    } finally {
      setSaving(null);
    }
  };

  if (!me.is_admin) {
    return (
      <div className="page-narrow">
        <h1 className="page-title">Settings</h1>
        <p className="page-sub">Only admins can change settings.</p>
      </div>
    );
  }

  return (
    <div className="page-narrow">
      <h1 className="page-title">Settings</h1>

      <section className="card mt-6">
        <h2 className="section-title">Audit log</h2>
        {loadErr && <div className="mt-3"><ErrorState message={loadErr} onRetry={load} /></div>}
        {!audit && !loadErr && <div className="skeleton mt-3 h-24 rounded-md" aria-hidden />}
        {audit && (
          <>
            <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
              Currently <span className="font-medium text-zinc-900 dark:text-zinc-100">{effective(audit)}</span>.
            </p>
            <div role="radiogroup" aria-label="Audit log mode" className="mt-4 space-y-2">
              {MODES.map((m) => {
                const active = audit.mode === m.value;
                return (
                  <button
                    key={m.value}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    disabled={saving !== null}
                    onClick={() => choose(m.value)}
                    className={clsx(
                      'flex w-full items-start gap-3 rounded-lg border px-3 py-2.5 text-left transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-60',
                      active
                        ? 'border-accent bg-accent/5 dark:bg-accent/10'
                        : 'border-zinc-200 hover:bg-zinc-50 active:bg-zinc-100 dark:border-zinc-800 dark:hover:bg-zinc-800/60',
                    )}
                  >
                    <span
                      aria-hidden
                      className={clsx(
                        'mt-0.5 h-4 w-4 shrink-0 rounded-full border',
                        active ? 'border-[0.3125rem] border-accent' : 'border-zinc-300 dark:border-zinc-600',
                      )}
                    />
                    <span>
                      <span className="block text-sm font-medium">{m.label}{saving === m.value && ' (saving…)'}</span>
                      <span className="block text-xs text-zinc-500">{m.hint}</span>
                    </span>
                  </button>
                );
              })}
            </div>
          </>
        )}
      </section>

      <section className="card mt-6">
        <h2 className="section-title">Set on the leader</h2>
        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
          These follow environment variables on the leader service and change only with a redeploy.
        </p>
        <dl className="mt-3 text-sm">
          <div className="flex justify-between border-b border-zinc-100 py-2 dark:border-zinc-800">
            <dt className="font-mono text-xs text-zinc-500">BPM_FILEBROWSER</dt>
            <dd>{me.filebrowser ? 'File browser on' : 'File browser off'}</dd>
          </div>
          <div className="flex justify-between border-b border-zinc-100 py-2 dark:border-zinc-800">
            <dt className="font-mono text-xs text-zinc-500">BPM_OIDC_USER_GROUP</dt>
            <dd className="font-mono text-sm">{me.user_group}</dd>
          </div>
          <div className="flex justify-between py-2">
            <dt className="font-mono text-xs text-zinc-500">BPM_OIDC_ADMIN_GROUP</dt>
            <dd className="font-mono text-sm">{me.admin_group}</dd>
          </div>
        </dl>
      </section>

    </div>
  );
}
