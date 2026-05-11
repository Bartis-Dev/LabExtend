import { useEffect, useState } from 'react';
import { Modal } from './Modal';
import { useCategories, useCreateService, useUpdateService } from '@/api/queries';
import type { Service, ServiceInput } from '@/api/types';
import { ApiError } from '@/api/client';

type Props = {
  open: boolean;
  onClose: () => void;
  initial?: Service;
};

function emptyInput(): ServiceInput {
  return {
    name: '',
    description: '',
    host_primary: '',
    port_primary: null,
    host_alt: null,
    port_alt: null,
    category_id: null,
    layout: { x: 0, y: 0, w: 1, h: 1 },
    ping_primary: false,
    ping_alt: false,
    hc_primary_enabled: false,
    hc_primary_url: null,
    hc_alt_enabled: false,
    hc_alt_url: null,
  };
}

function fromService(s: Service): ServiceInput {
  return {
    name: s.name,
    description: s.description,
    host_primary: s.host_primary,
    port_primary: s.port_primary ?? null,
    host_alt: s.host_alt ?? null,
    port_alt: s.port_alt ?? null,
    category_id: s.category_id ?? null,
    layout: s.layout,
    ping_primary: s.ping_primary,
    ping_alt: s.ping_alt,
    hc_primary_enabled: s.hc_primary_enabled,
    hc_primary_url: s.hc_primary_url ?? null,
    hc_alt_enabled: s.hc_alt_enabled,
    hc_alt_url: s.hc_alt_url ?? null,
  };
}

export function ServiceForm({ open, onClose, initial }: Props) {
  const [form, setForm] = useState<ServiceInput>(emptyInput);
  const [error, setError] = useState<string | null>(null);
  const create = useCreateService();
  const update = useUpdateService();
  const cats = useCategories();

  useEffect(() => {
    if (!open) return;
    setError(null);
    setForm(initial ? fromService(initial) : emptyInput());
  }, [open, initial]);

  const set = <K extends keyof ServiceInput>(key: K, value: ServiceInput[K]) =>
    setForm((f) => ({ ...f, [key]: value }));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      if (initial) {
        await update.mutateAsync({ id: initial.id, input: form });
      } else {
        await create.mutateAsync(form);
      }
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'request failed');
    }
  };

  const busy = create.isPending || update.isPending;

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={initial ? `Edit ${initial.name}` : 'Add service'}
      size="md"
    >
      <form onSubmit={submit} className="space-y-4">
        <Row>
          <Field label="Name" value={form.name} onChange={(v) => set('name', v)} required />
          <CategoryField
            value={form.category_id ?? null}
            onChange={(v) => set('category_id', v)}
            options={cats.data ?? []}
          />
        </Row>

        <Field
          label="Description"
          value={form.description}
          onChange={(v) => set('description', v)}
        />

        <Section title="Primary host">
          <Row>
            <Field
              label="Host or URL"
              value={form.host_primary}
              onChange={(v) => set('host_primary', v)}
              required
            />
            <NumField
              label="Port (optional)"
              value={form.port_primary ?? null}
              onChange={(v) => set('port_primary', v)}
            />
          </Row>
          <Toggle
            label="Enable ping (TCP)"
            value={form.ping_primary}
            onChange={(v) => set('ping_primary', v)}
          />
          <Toggle
            label="Enable HTTP healthcheck"
            value={form.hc_primary_enabled}
            onChange={(v) => set('hc_primary_enabled', v)}
          />
          {form.hc_primary_enabled && (
            <Field
              label="Healthcheck URL (defaults to primary host)"
              value={form.hc_primary_url ?? ''}
              onChange={(v) => set('hc_primary_url', v || null)}
            />
          )}
        </Section>

        <Section title="Alternative host (optional)">
          <Row>
            <Field
              label="Host or URL"
              value={form.host_alt ?? ''}
              onChange={(v) => set('host_alt', v || null)}
            />
            <NumField
              label="Port (optional)"
              value={form.port_alt ?? null}
              onChange={(v) => set('port_alt', v)}
            />
          </Row>
          <Toggle
            label="Enable ping"
            value={form.ping_alt}
            onChange={(v) => set('ping_alt', v)}
          />
          <Toggle
            label="Enable HTTP healthcheck"
            value={form.hc_alt_enabled}
            onChange={(v) => set('hc_alt_enabled', v)}
          />
          {form.hc_alt_enabled && (
            <Field
              label="Healthcheck URL"
              value={form.hc_alt_url ?? ''}
              onChange={(v) => set('hc_alt_url', v || null)}
            />
          )}
        </Section>

        {error && (
          <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
            {error}
          </div>
        )}

        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-border px-4 py-2 hover:bg-bg-elevated"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={busy}
            className="rounded bg-accent px-4 py-2 text-white hover:bg-accent-hover disabled:opacity-50"
          >
            {busy ? '…' : initial ? 'Save' : 'Create'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function Row({ children }: { children: React.ReactNode }) {
  return <div className="grid grid-cols-2 gap-3">{children}</div>;
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <fieldset className="rounded border border-border bg-bg-elevated/40 p-3">
      <legend className="px-1 text-xs uppercase tracking-wide text-fg-muted">{title}</legend>
      <div className="space-y-3">{children}</div>
    </fieldset>
  );
}

function Field({
  label,
  value,
  onChange,
  required,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  required?: boolean;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs text-fg-muted">{label}</span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        required={required}
        className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
      />
    </label>
  );
}

function NumField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number | null;
  onChange: (v: number | null) => void;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs text-fg-muted">{label}</span>
      <input
        type="number"
        value={value ?? ''}
        onChange={(e) => {
          const v = e.target.value;
          onChange(v === '' ? null : Number(v));
        }}
        min={1}
        max={65535}
        className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
      />
    </label>
  );
}

function Toggle({
  label,
  value,
  onChange,
}: {
  label: string;
  value: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer items-center gap-2 text-sm">
      <input
        type="checkbox"
        checked={value}
        onChange={(e) => onChange(e.target.checked)}
        className="h-4 w-4 accent-accent"
      />
      {label}
    </label>
  );
}

function CategoryField({
  value,
  onChange,
  options,
}: {
  value: number | null;
  onChange: (v: number | null) => void;
  options: { id: number; name: string }[];
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs text-fg-muted">Category (optional)</span>
      <select
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value ? Number(e.target.value) : null)}
        className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
      >
        <option value="">— None —</option>
        {options.map((c) => (
          <option key={c.id} value={c.id}>
            {c.name}
          </option>
        ))}
      </select>
    </label>
  );
}
