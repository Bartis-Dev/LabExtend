import { useRef, useState } from 'react';
import { EditIcon, ExternalLinkIcon, GripIcon, TrashIcon } from './icons';
import { ConfirmDialog } from './Modal';
import { ServiceForm } from './ServiceForm';
import { useDeleteService, useHealth } from '@/api/queries';
import type { HostStatus, Service } from '@/api/types';
import { writePayload } from './Dashboard/crossGridDnd';

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
  const noScheme = host.replace(/^https?:\/\//i, '');
  if (port && !noScheme.includes(':')) return `${noScheme}:${port}`;
  return noScheme;
}

function StatusDot({ status }: { status: HostStatus | undefined }) {
  const cls =
    status === 'up'
      ? 'bg-success'
      : status === 'down'
        ? 'bg-danger'
        : 'bg-fg-muted/30';
  const label = status === 'up' ? 'Online' : status === 'down' ? 'Offline' : 'Not monitored';
  return <span className={`inline-block h-2 w-2 shrink-0 rounded-full ${cls}`} aria-label={label} title={label} />;
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
        className="h-8 w-8 shrink-0 rounded-md border border-border/60 bg-bg-elevated object-cover"
        onError={(e) => ((e.currentTarget as HTMLImageElement).style.display = 'none')}
      />
    );
  }
  const letter = name.trim().charAt(0).toUpperCase() || '?';
  return (
    <div className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-gradient-to-br from-accent to-accent-hover text-sm font-bold text-white">
      {letter}
    </div>
  );
}

// ServiceCard is sized by react-grid-layout to fit its current cell. The
// content is laid out so the smallest sensible footprint (1x1) still shows
// avatar + name + primary host. Description and alt host live below and are
// naturally hidden by overflow:hidden when the card is too short to show
// them. Bigger cells reveal more — no JS branching on size.
export function ServiceCard({ service }: { service: Service }) {
  const [editOpen, setEditOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const cardRef = useRef<HTMLDivElement | null>(null);
  const del = useDeleteService();
  const health = useHealth();
  const status = health.data?.[service.id];

  const handleDragStart = (e: React.DragEvent) => {
    writePayload(e, {
      id: service.id,
      w: service.layout.w,
      h: service.layout.h,
      fromCategoryId: service.category_id ?? null,
    });
    // Render the whole card as the drag image rather than just the grip icon.
    if (cardRef.current) {
      e.dataTransfer.setDragImage(cardRef.current, 20, 20);
    }
  };

  return (
    <div
      ref={cardRef}
      className="service-card group relative flex h-full flex-col overflow-hidden rounded-lg border border-border bg-bg-card p-2.5 shadow-sm transition-colors hover:border-border-strong"
    >
      {/* Action bar — visible on hover */}
      <div className="absolute right-1.5 top-1.5 z-10 flex items-center gap-0.5 rounded bg-bg-elevated/90 opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100">
        <button
          onClick={(e) => {
            e.stopPropagation();
            setEditOpen(true);
          }}
          onMouseDown={(e) => e.stopPropagation()}
          className="rounded p-1 text-fg-muted hover:bg-bg-card hover:text-fg"
          aria-label="Edit"
        >
          <EditIcon width={13} height={13} />
        </button>
        <button
          onClick={(e) => {
            e.stopPropagation();
            setConfirmOpen(true);
          }}
          onMouseDown={(e) => e.stopPropagation()}
          className="rounded p-1 text-fg-muted hover:bg-bg-card hover:text-danger"
          aria-label="Delete"
        >
          <TrashIcon width={13} height={13} />
        </button>
      </div>

      {/* Cross-grid drag handle — visible on hover. HTML5 native DnD so it
          coexists with react-grid-layout's mouse-driven within-grid drag. */}
      <div
        draggable
        onDragStart={handleDragStart}
        onMouseDown={(e) => e.stopPropagation()}
        className="absolute left-1.5 top-1.5 z-10 cursor-grab rounded bg-bg-elevated/90 p-0.5 text-fg-muted opacity-0 backdrop-blur-sm transition-opacity hover:text-fg group-hover:opacity-100 active:cursor-grabbing"
        aria-label="Drag between categories"
        title="Drag to another category or outside"
      >
        <GripIcon width={13} height={13} />
      </div>

      {/* Header */}
      <div className="flex items-center gap-2">
        <Avatar name={service.name} icon={service.icon_path} />
        <div className="min-w-0 flex-1 pr-12">
          <div className="truncate text-sm font-semibold leading-tight">
            {service.name}
          </div>
        </div>
      </div>

      {service.description && (
        <div className="mt-1.5 line-clamp-2 text-[11px] leading-snug text-fg-muted">
          {service.description}
        </div>
      )}

      {/* Hosts */}
      <div className="mt-auto flex flex-col gap-1 pt-2">
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
      className={`flex items-center gap-1.5 truncate rounded border border-border/60 bg-bg-elevated/50 px-2 py-1 font-mono text-[11px] transition-colors hover:border-accent hover:bg-bg-elevated ${
        primary ? 'text-fg' : 'text-fg-muted'
      }`}
    >
      <StatusDot status={status} />
      <span className="flex-1 truncate">{display}</span>
      <ExternalLinkIcon width={10} height={10} className="shrink-0 text-fg-muted opacity-50" />
    </a>
  );
}
