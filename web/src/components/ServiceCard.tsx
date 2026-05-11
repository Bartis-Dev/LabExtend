import { useState } from 'react';
import { EditIcon, ExternalLinkIcon, TrashIcon } from './icons';
import { ConfirmDialog } from './Modal';
import { ServiceForm } from './ServiceForm';
import { useDeleteService } from '@/api/queries';
import { useHealth } from '@/api/queries';
import type { HostStatus, Service } from '@/api/types';

function hostHref(host: string, port?: number | null): string {
  // If host already has a scheme, return as-is.
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
  // No scheme: assume http and add port if present.
  return `http://${host}${port ? `:${port}` : ''}`;
}

function StatusDot({ status }: { status: HostStatus | undefined }) {
  const color =
    status === 'up'
      ? 'bg-success'
      : status === 'down'
        ? 'bg-danger'
        : 'bg-fg-muted/30';
  return <span className={`inline-block h-2 w-2 rounded-full ${color}`} aria-hidden />;
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
        className="h-8 w-8 rounded object-cover"
        onError={(e) => ((e.currentTarget as HTMLImageElement).style.display = 'none')}
      />
    );
  }
  const letter = name.trim().charAt(0).toUpperCase() || '?';
  return (
    <div className="grid h-8 w-8 place-items-center rounded bg-accent font-bold text-white">
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
    <div className="flex h-full flex-col rounded-lg border border-border bg-bg-card p-3 shadow-sm">
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <Avatar name={service.name} icon={service.icon_path} />
          <div className="min-w-0">
            <div className="truncate font-semibold leading-tight">{service.name}</div>
            {service.description && (
              <div className="truncate text-xs text-fg-muted">{service.description}</div>
            )}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <button
            onClick={(e) => {
              e.stopPropagation();
              setEditOpen(true);
            }}
            onMouseDown={(e) => e.stopPropagation()}
            className="rounded p-1 text-fg-muted hover:bg-bg-elevated hover:text-fg"
            aria-label="Edit"
          >
            <EditIcon width={14} height={14} />
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation();
              setConfirmOpen(true);
            }}
            onMouseDown={(e) => e.stopPropagation()}
            className="rounded p-1 text-fg-muted hover:bg-bg-elevated hover:text-danger"
            aria-label="Delete"
          >
            <TrashIcon width={14} height={14} />
          </button>
        </div>
      </div>

      <div className="mt-2 flex flex-col gap-1 text-sm">
        <a
          href={hostHref(service.host_primary, service.port_primary)}
          target="_blank"
          rel="noopener noreferrer"
          onMouseDown={(e) => e.stopPropagation()}
          className="flex items-center gap-2 truncate text-fg hover:text-accent"
        >
          <StatusDot status={status?.primary} />
          <span className="truncate">{service.host_primary}</span>
          <ExternalLinkIcon width={12} height={12} className="shrink-0 text-fg-muted" />
        </a>
        {service.host_alt && (
          <a
            href={hostHref(service.host_alt, service.port_alt)}
            target="_blank"
            rel="noopener noreferrer"
            onMouseDown={(e) => e.stopPropagation()}
            className="flex items-center gap-2 truncate text-fg-muted hover:text-accent"
          >
            <StatusDot status={status?.alt} />
            <span className="truncate">{service.host_alt}</span>
            <ExternalLinkIcon width={12} height={12} className="shrink-0" />
          </a>
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
