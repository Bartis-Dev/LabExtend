import { useEffect, useRef, useState } from 'react';
import { Modal } from './Modal';
import { IconPicker } from './IconPicker';
import {
  servicesKey,
  useCategories,
  useCreateService,
  useDeleteIcon,
  useSetIconURL,
  useUpdateService,
} from '@/api/queries';
import { ApiError, uploadFile } from '@/api/client';
import { useQueryClient } from '@tanstack/react-query';
import { FolderIcon, UploadIcon } from './icons';
import type { Service, ServiceInput } from '@/api/types';

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
    icon_path: null,
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
    icon_path: s.icon_path ?? null,
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
  const [iconUploading, setIconUploading] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const fileRef = useRef<HTMLInputElement | null>(null);
  const create = useCreateService();
  const update = useUpdateService();
  const setIconURL = useSetIconURL();
  const deleteIcon = useDeleteIcon();
  const cats = useCategories();
  const qc = useQueryClient();

  const iconPath = form.icon_path ?? null;

  useEffect(() => {
    if (!open) return;
    setError(null);
    setForm(initial ? fromService(initial) : emptyInput());
  }, [open, initial]);

  const handleIconFile = async (file: File) => {
    if (!initial) {
      setError('Save the service first, then upload an icon by editing it.');
      return;
    }
    setIconUploading(true);
    setError(null);
    try {
      const res = await uploadFile<{ icon_path: string }>(
        `/api/services/${initial.id}/icon`,
        file,
      );
      setForm((f) => ({ ...f, icon_path: res.icon_path }));
      qc.invalidateQueries({ queryKey: servicesKey });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'upload failed');
    } finally {
      setIconUploading(false);
    }
  };

  const handlePickIcon = async (url: string) => {
    if (initial) {
      // Existing service — push the URL right away so the card updates live.
      try {
        await setIconURL.mutateAsync({ id: initial.id, url });
      } catch (err) {
        setError(err instanceof ApiError ? err.message : 'icon save failed');
        return;
      }
    }
    setForm((f) => ({ ...f, icon_path: url }));
  };

  const handleIconRemove = async () => {
    if (!initial) {
      setForm((f) => ({ ...f, icon_path: null }));
      return;
    }
    try {
      await deleteIcon.mutateAsync(initial.id);
      setForm((f) => ({ ...f, icon_path: null }));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'remove failed');
    }
  };

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
      size="lg"
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

        <IconRow
          iconPath={iconPath}
          fileRef={fileRef}
          uploading={iconUploading}
          onBrowse={() => setPickerOpen(true)}
          onChooseFile={() => fileRef.current?.click()}
          onFileChange={(f) => f && handleIconFile(f)}
          onRemove={handleIconRemove}
          editing={!!initial}
        />

        <Section title="Primary host">
          <Row>
            <Field
              label="Host or URL"
              value={form.host_primary}
              onChange={(v) => set('host_primary', v)}
              required
              placeholder="https://app.lan or app.lan or 192.168.1.50"
            />
            <NumField
              label="Port (optional)"
              value={form.port_primary ?? null}
              onChange={(v) => set('port_primary', v)}
              placeholder="e.g. 25565 for Minecraft"
            />
          </Row>
          <p className="text-xs text-fg-muted">
            Scheme defaults: <code>https://</code> → 443, <code>http://</code> → 80, no scheme → 80.
            Set the port field to override (e.g. Minecraft on 25565).
          </p>
          <Toggle
            label="Enable ping (TCP connect)"
            value={form.ping_primary}
            onChange={(v) => set('ping_primary', v)}
            hint="Works for any TCP service — Minecraft, SSH, databases, HTTP."
          />
          <Toggle
            label="Enable HTTP healthcheck (2xx/3xx = up)"
            value={form.hc_primary_enabled}
            onChange={(v) => set('hc_primary_enabled', v)}
            hint="Only for HTTP(S) services. Skip this for Minecraft etc."
          />
          {form.hc_primary_enabled && (
            <Field
              label="Healthcheck URL override (default: derived from host above)"
              value={form.hc_primary_url ?? ''}
              onChange={(v) => set('hc_primary_url', v || null)}
              placeholder="https://app.lan/health"
            />
          )}
        </Section>

        <Section title="Alternative host (optional)">
          <Row>
            <Field
              label="Host or URL"
              value={form.host_alt ?? ''}
              onChange={(v) => set('host_alt', v || null)}
              placeholder="e.g. internal IP for the same service"
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

      <IconPicker
        open={pickerOpen}
        onClose={() => setPickerOpen(false)}
        onPick={handlePickIcon}
      />
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
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  required?: boolean;
  placeholder?: string;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs text-fg-muted">{label}</span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        required={required}
        placeholder={placeholder}
        className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
      />
    </label>
  );
}

function NumField({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: number | null;
  onChange: (v: number | null) => void;
  placeholder?: string;
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
        placeholder={placeholder}
        className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
      />
    </label>
  );
}

function Toggle({
  label,
  value,
  onChange,
  hint,
}: {
  label: string;
  value: boolean;
  onChange: (v: boolean) => void;
  hint?: string;
}) {
  return (
    <div>
      <label className="flex cursor-pointer items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={value}
          onChange={(e) => onChange(e.target.checked)}
          className="h-4 w-4 accent-accent"
        />
        {label}
      </label>
      {hint && <p className="ml-6 mt-0.5 text-xs text-fg-muted">{hint}</p>}
    </div>
  );
}

function previewUrl(iconPath: string | null): string | null {
  if (!iconPath) return null;
  if (/^https?:\/\//i.test(iconPath)) return iconPath;
  return `/api/icons/${iconPath.replace(/^icons\//, '')}`;
}

function IconRow({
  iconPath,
  fileRef,
  uploading,
  onBrowse,
  onChooseFile,
  onFileChange,
  onRemove,
  editing,
}: {
  iconPath: string | null;
  fileRef: React.RefObject<HTMLInputElement>;
  uploading: boolean;
  onBrowse: () => void;
  onChooseFile: () => void;
  onFileChange: (f: File | null) => void;
  onRemove: () => void;
  editing: boolean;
}) {
  const url = previewUrl(iconPath);
  return (
    <div className="flex flex-wrap items-center gap-3 rounded border border-border bg-bg-elevated/40 p-3">
      <div className="grid h-12 w-12 shrink-0 place-items-center rounded bg-bg-elevated">
        {url ? (
          <img src={url} alt="" className="h-12 w-12 rounded object-contain" />
        ) : (
          <span className="text-fg-muted">—</span>
        )}
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">Icon</div>
        <div className="text-xs text-fg-muted">
          Pick from the catalogue or upload your own (PNG / JPG / WebP / SVG, max 2 MiB).
          {!editing && ' Upload becomes available once the service is saved.'}
        </div>
      </div>
      <input
        ref={fileRef}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/svg+xml"
        className="hidden"
        onChange={(e) => onFileChange(e.target.files?.[0] ?? null)}
      />
      <button
        type="button"
        onClick={onBrowse}
        className="flex items-center gap-1 rounded border border-border px-3 py-1.5 text-sm hover:bg-bg-elevated"
      >
        <FolderIcon width={14} height={14} /> Browse
      </button>
      <button
        type="button"
        onClick={onChooseFile}
        disabled={!editing || uploading}
        className="flex items-center gap-1 rounded border border-border px-3 py-1.5 text-sm hover:bg-bg-elevated disabled:opacity-50"
      >
        <UploadIcon width={14} height={14} /> {uploading ? '…' : 'Upload'}
      </button>
      {iconPath && (
        <button
          type="button"
          onClick={onRemove}
          className="text-sm text-fg-muted hover:text-danger"
        >
          Remove
        </button>
      )}
    </div>
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
