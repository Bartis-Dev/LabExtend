import { useEffect, useState } from 'react';
import { ThemeEditor } from '@/components/theme/ThemeEditor';
import {
  useSettings,
  useUpdateSettings,
} from '@/api/queries';
import { api, ApiError } from '@/api/client';

export default function Settings() {
  return (
    <div className="mx-auto max-w-5xl space-y-10 p-6">
      <Section title="Theme">
        <ThemeEditor />
      </Section>
      <Section title="Layout">
        <LayoutSettings />
      </Section>
      <Section title="Healthcheck">
        <HealthcheckSettings />
      </Section>
      <Section title="Account">
        <PasswordChange />
      </Section>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <h2 className="mb-3 text-xl font-bold">{title}</h2>
      <div className="rounded-lg border border-border bg-bg-card/40 p-5">{children}</div>
    </section>
  );
}

// --- Layout ---------------------------------------------------------------

function LayoutSettings() {
  const settings = useSettings();
  const update = useUpdateSettings();
  const [cols, setCols] = useState('6');
  const [msg, setMsg] = useState<string | null>(null);

  useEffect(() => {
    if (settings.data?.grid_cols) setCols(settings.data.grid_cols);
  }, [settings.data]);

  const save = async () => {
    setMsg(null);
    try {
      await update.mutateAsync({ ...(settings.data ?? {}), grid_cols: cols });
      setMsg('Saved.');
    } catch (err) {
      setMsg(err instanceof ApiError ? err.message : 'save failed');
    }
  };

  return (
    <div className="flex items-end gap-3">
      <label className="block">
        <span className="mb-1 block text-xs text-fg-muted">Grid columns</span>
        <select
          value={cols}
          onChange={(e) => setCols(e.target.value)}
          className="w-32 rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
        >
          {[4, 5, 6, 7, 8, 9, 10, 11, 12].map((n) => (
            <option key={n} value={String(n)}>
              {n}
            </option>
          ))}
        </select>
      </label>
      <button
        onClick={save}
        disabled={update.isPending}
        className="rounded bg-accent px-4 py-2 text-white hover:bg-accent-hover disabled:opacity-50"
      >
        Save
      </button>
      {msg && <span className="text-sm text-fg-muted">{msg}</span>}
    </div>
  );
}

// --- Healthcheck ----------------------------------------------------------

function HealthcheckSettings() {
  const settings = useSettings();
  const update = useUpdateSettings();
  const [interval, setInterval] = useState('60s');
  const [msg, setMsg] = useState<string | null>(null);

  useEffect(() => {
    if (settings.data?.healthcheck_interval) setInterval(settings.data.healthcheck_interval);
  }, [settings.data]);

  const save = async () => {
    setMsg(null);
    try {
      await update.mutateAsync({ ...(settings.data ?? {}), healthcheck_interval: interval });
      setMsg('Saved.');
    } catch (err) {
      setMsg(err instanceof ApiError ? err.message : 'save failed');
    }
  };

  return (
    <div className="flex items-end gap-3">
      <label className="block">
        <span className="mb-1 block text-xs text-fg-muted">
          Probe interval (Go duration, 10s–1h)
        </span>
        <input
          value={interval}
          onChange={(e) => setInterval(e.target.value)}
          placeholder="60s"
          className="w-40 rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
        />
      </label>
      <button
        onClick={save}
        disabled={update.isPending}
        className="rounded bg-accent px-4 py-2 text-white hover:bg-accent-hover disabled:opacity-50"
      >
        Save
      </button>
      {msg && <span className="text-sm text-fg-muted">{msg}</span>}
    </div>
  );
}

// --- Password change ------------------------------------------------------

function PasswordChange() {
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setMsg(null);
    setErr(null);
    setBusy(true);
    try {
      await api.put('/api/auth/password', {
        current,
        new: next,
        new_confirm: confirm,
      });
      setCurrent('');
      setNext('');
      setConfirm('');
      setMsg('Password updated.');
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'request failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="grid max-w-md grid-cols-1 gap-3">
      <PwField label="Current password" value={current} onChange={setCurrent} />
      <PwField label="New password" value={next} onChange={setNext} />
      <PwField label="Confirm new password" value={confirm} onChange={setConfirm} />
      {msg && <div className="text-sm text-success">{msg}</div>}
      {err && (
        <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
          {err}
        </div>
      )}
      <button
        type="submit"
        disabled={busy}
        className="w-fit rounded bg-accent px-4 py-2 text-white hover:bg-accent-hover disabled:opacity-50"
      >
        {busy ? '…' : 'Change password'}
      </button>
    </form>
  );
}

function PwField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs text-fg-muted">{label}</span>
      <input
        type="password"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        required
        className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
      />
    </label>
  );
}
