import { useState } from 'react';
import { EditIcon, ExternalLinkIcon, GripIcon, TrashIcon } from './icons';
import { ConfirmDialog } from './Modal';
import { ServiceForm } from './ServiceForm';
import { ContextMenu, useContextMenu } from './ContextMenu';
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
  const noScheme = host.replace(/^https?:\/\//i, '');
  if (port && !noScheme.includes(':')) return `${noScheme}:${port}`;
  return noScheme;
}

function protocolLabel(host: string): string {
  if (/^https:\/\//i.test(host)) return 'SSL';
  if (/^http:\/\//i.test(host)) return 'WEB';
  return 'TCP';
}

function statusLabel(status: HostStatus | undefined): string {
  if (status === 'up') return 'Online';
  if (status === 'down') return 'Offline';
  return 'Not monitored';
}

function statusDotColor(status: HostStatus | undefined): string {
  if (status === 'up') return 'bg-success';
  if (status === 'down') return 'bg-danger';
  return 'bg-fg-muted/40';
}

function iconUrl(p?: string | null): string | null {
  if (!p) return null;
  if (/^https?:\/\//i.test(p)) return p;
  const name = p.startsWith('icons/') ? p.slice('icons/'.length) : p;
  return `/api/icons/${name}`;
}

function Avatar({ icon }: { icon?: string | null }) {
  const url = iconUrl(icon);
  if (!url) {
    // Invisible spacer keeps every card header aligned even when no
    // icon is set, so a column of cards reads as a tidy grid.
    return <div className="h-8 w-8 shrink-0" aria-hidden />;
  }
  return (
    <img
      src={url}
      alt=""
      className="h-8 w-8 shrink-0 rounded-md border border-border/60 bg-bg-elevated object-cover"
      onError={(e) => ((e.currentTarget as HTMLImageElement).style.display = 'none')}
    />
  );
}

export function ServiceCard({ service }: { service: Service }) {
  const [editOpen, setEditOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const del = useDeleteService();
  const health = useHealth();
  const status = health.data?.[service.id];
  const menu = useContextMenu();

  return (
    <div
      className="service-card relative flex h-full flex-col rounded-lg border border-border bg-bg-card p-3.5 shadow-md shadow-black/30"
      onContextMenu={menu.onContextMenu}
    >
      {/* Drag handle — top right */}
      <div
        className="rgl-drag-handle absolute right-2 top-2 z-10 cursor-grab rounded p-1 text-fg-muted/60 hover:bg-bg-elevated hover:text-fg active:cursor-grabbing"
        title="Drag to reorder"
        onContextMenu={(e) => e.stopPropagation()}
      >
        <GripIcon width={13} height={13} />
      </div>

      {/* Header — avatar slot always reserved (invisible if no icon),
          description slot always rendered (NBSP keeps the height stable
          when empty). Result: every card has identical header geometry. */}
      <div className="flex items-center gap-3 pr-8">
        <Avatar icon={service.icon_path} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold leading-tight">
            {service.name}
          </div>
          <div className="mt-1 truncate text-[11px] leading-4 text-fg-muted">
            {service.description || ' '}
          </div>
        </div>
      </div>

      {/* Hosts — always two slots so cards with one host align with cards
          that have two. The alt slot is a faded placeholder when no alt
          host is configured. */}
      <div className="mt-3.5 flex flex-col items-start gap-2">
        <HostRow
          href={hostHref(service.host_primary, service.port_primary)}
          display={hostDisplay(service.host_primary, service.port_primary)}
          protocol={protocolLabel(service.host_primary)}
          status={status?.primary}
        />
        {service.host_alt ? (
          <HostRow
            href={hostHref(service.host_alt, service.port_alt)}
            display={hostDisplay(service.host_alt, service.port_alt)}
            protocol={protocolLabel(service.host_alt)}
            status={status?.alt}
            secondary
          />
        ) : (
          <HostRowPlaceholder />
        )}
      </div>

      {/* Reserved space for future per-card widgets. */}
      <div className="flex-1" />

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

      <ContextMenu
        open={menu.open}
        x={menu.x}
        y={menu.y}
        onClose={menu.close}
        items={[
          {
            label: 'Edit',
            icon: <EditIcon width={14} height={14} />,
            onClick: () => setEditOpen(true),
          },
          {
            label: 'Delete',
            icon: <TrashIcon width={14} height={14} />,
            onClick: () => setConfirmOpen(true),
            danger: true,
          },
        ]}
      />
    </div>
  );
}

// HostRowPlaceholder reserves the same physical space as a HostRow so
// cards line up vertically regardless of how many hosts each service
// configures. Dashed border + muted dash signals "intentionally empty",
// not "broken".
function HostRowPlaceholder() {
  return (
    <div
      aria-hidden
      className="inline-flex h-9 min-w-[14rem] max-w-full items-center justify-center rounded-md border border-dashed border-border/40 px-3 font-mono text-xs text-fg-muted/40"
    >
      —
    </div>
  );
}

function HostRow({
  href,
  display,
  protocol,
  status,
  secondary,
}: {
  href: string;
  display: string;
  protocol: string;
  status: HostStatus | undefined;
  secondary?: boolean;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      onMouseDown={(e) => e.stopPropagation()}
      onContextMenu={(e) => e.stopPropagation()}
      aria-label={`${display} (${statusLabel(status)}, ${protocol})`}
      className={`group/host inline-flex h-9 min-w-[14rem] max-w-full items-stretch overflow-hidden rounded-md border bg-bg-elevated/50 text-fg transition-colors hover:border-accent ${
        secondary ? 'border-border/50' : 'border-border-strong/60'
      }`}
    >
      <span className="flex shrink-0 items-center pl-2.5">
        <span className={`h-2 w-2 rounded-full ${statusDotColor(status)}`} title={statusLabel(status)} />
      </span>
      <span className="flex shrink-0 items-center border-r border-border/40 px-2 font-mono text-[10px] font-medium uppercase tracking-wider text-fg-muted">
        {protocol}
      </span>
      <span className="flex flex-1 items-center truncate px-3 font-mono text-xs">
        {display}
      </span>
      <span className="flex items-center pr-2.5">
        <ExternalLinkIcon
          width={11}
          height={11}
          className="text-fg-muted opacity-40 transition-opacity group-hover/host:opacity-100"
        />
      </span>
    </a>
  );
}
