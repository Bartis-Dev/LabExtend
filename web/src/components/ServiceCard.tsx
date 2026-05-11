import { useState } from 'react';
import { EditIcon, ExternalLinkIcon, TrashIcon } from './icons';
import { ConfirmDialog } from './Modal';
import { ServiceForm } from './ServiceForm';
import { useDeleteService, useHealth } from '@/api/queries';
import type { HostStatus, Service } from '@/api/types';

function hostHref(host: string, port?: number | null): string {
  if (/^https?:\/\//i.test(host)) {
    if (port) {
      try {
        const u = new URL(host);
        u.port = String(port);
        return u.toString();
      } catch {
        return host;
      }
    }
    return host;
  }
  return `http://${host}${port ? `:${port}` : ''}`;
}

function hostDisplay(host: string, port?: number | null): string {
  // Strip the scheme so the card stays readable; keep the port if present.
  const noScheme = host.replace(/^https?:\/\//i, '');
  if (port && !noScheme.includes(':')) return `${noScheme}:${port}`;
  return noScheme;
}

function StatusDot({ status }: { status: HostStatus | undefined }) {
  const cls =
    status === 'up'
      ? 'bg-success ring-2 ring-success/30'
      : status === 'down'
        ? 'bg-danger ring-2 ring-danger/30'
        : 'bg-fg-muted/30';
  const label = status === 'up' ? 'Online' : status === 'down' ? 'Offline' : 'Not monitored';
  return <span className={`inline-block h-2.5 w-2.5 shrink-0 rounded-full ${cls}`} aria-label={label} title={label} />;
}

function iconUrl(p?: string | null): string | null {
  if (!p) return null;
  const name = p.startsWith('icons/') ? p.slice('icons/'.length) : p;
  return `/api/icons/${name}`;
}

function Avatar({ name, icon }: { name: string; icon?: string | null }) {
  const url = iconUrl(icon);
  if (url) {
    return (
      <img
        src={url}
        alt=""
        className="h-12 w-12 shrink-0 rounded-lg border border-border bg-bg-elevated object-cover"
        onError={(e) => ((e.currentTarget as HTMLImageElement).style.display = 'none')}
      />
    );
  }
  const letter = name.trim().charAt(0).toUpperCase() || '?';
  return (
    <div className="grid h-12 w-12 shrink-0 place-items-center rounded-lg bg-gradient-to-br from-accent to-accent-hover text-xl font-bold text-white shadow-md">
      {letter}
    </div>
  );
}

export function ServiceCard({ service }: { service: Service }) {
  const [editOpen, setEditOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const del = useDeleteService();
  const health = useHealth();
  const status = health.data?.[service.id];

  return (
    <div className="group relative flex h-full min-h-[120px] flex-col rounded-xl border border-border bg-bg-card p-4 shadow-sm transition-colors hover:border-border-strong">
      {/* Action bar — appears on hover, doesn't shift layout */}
      <div className="absolute right-2 top-2 flex items-center gap-0.5 rounded-md bg-bg-elevated/80 p-0.5 opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100">
        <button
          onClick={(e) => {
            e.stopPropagation();
            setEditOpen(true);
          }}
          onMouseDown={(e) => e.stopPropagation()}
          className="rounded p-1.5 text-fg-muted hover:bg-bg-card hover:text-fg"
          aria-label="Edit"
        >
          <EditIcon width={15} height={15} />
        </button>
        <button
          onClick={(e) => {
            e.stopPropagation();
            setConfirmOpen(true);
          }}
          onMouseDown={(e) => e.stopPropagation()}
          className="rounded p-1.5 text-fg-muted hover:bg-bg-card hover:text-danger"
          aria-label="Delete"
        >
          <TrashIcon width={15} height={15} />
        </button>
      </div>

      {/* Header */}
      <div className="flex items-start gap-3">
        <Avatar name={service.name} icon={service.icon_path} />
        <div className="min-w-0 flex-1">
          <div className="truncate pr-16 text-base font-semibold leading-tight">
            {service.name}
          </div>
          {service.description && (
            <div className="mt-1 line-clamp-2 text-xs leading-snug text-fg-muted">
              {service.description}
            </div>
          )}
        </div>
      </div>

      {/* Hosts */}
      <div className="mt-3 flex flex-1 flex-col gap-1.5">
        <HostRow
          href={hostHref(service.host_primary, service.port_primary)}
          display={hostDisplay(service.host_primary, service.port_primary)}
          status={status?.primary}
          primary
        />
        {service.host_alt && (
          <HostRow
            href={hostHref(service.host_alt, service.port_alt)}
            display={hostDisplay(service.host_alt, service.port_alt)}
            status={status?.alt}
          />
        )}
      </div>

      <ServiceForm open={editOpen} onClose={() => setEditOpen(false)} initial={service} />
      <ConfirmDialog
        open={confirmOpen}
        title="Delete service?"
        message={`Are you sure you want to delete "${service.name}"? This cannot be undone.`}
        onCancel={() => setConfirmOpen(false)}
        onConfirm={async () => {
          await del.mutateAsync(service.id);
          setConfirmOpen(false);
        }}
      />
    </div>
  );
}

function HostRow({
  href,
  display,
  status,
  primary,
}: {
  href: string;
  display: string;
  status: HostStatus | undefined;
  primary?: boolean;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      onMouseDown={(e) => e.stopPropagation()}
      className={`flex items-center gap-2 rounded-md border border-border bg-bg-elevated/60 px-2.5 py-1.5 text-sm transition-colors hover:border-accent hover:bg-bg-elevated ${
        primary ? 'text-fg' : 'text-fg-muted'
      }`}
    >
      <StatusDot status={status} />
      <span className="flex-1 truncate font-mono text-xs">{display}</span>
      <ExternalLinkIcon width={12} height={12} className="shrink-0 text-fg-muted opacity-60" />
    </a>
  );
}
