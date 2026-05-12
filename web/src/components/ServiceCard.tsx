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

// protocolLabel picks the 3-letter badge text:
//   https:// → SSL
//   http://  → WEB
//   no scheme → TCP (e.g. Minecraft, raw services)
function protocolLabel(host: string): string {
  if (/^https:\/\//i.test(host)) return 'SSL';
  if (/^http:\/\//i.test(host)) return 'WEB';
  return 'TCP';
}

function statusColor(status: HostStatus | undefined): string {
  if (status === 'up') return 'bg-success';
  if (status === 'down') return 'bg-danger';
  return 'bg-fg-muted/30';
}

function statusLabel(status: HostStatus | undefined): string {
  if (status === 'up') return 'Online';
  if (status === 'down') return 'Offline';
  return 'Not monitored';
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

export function ServiceCard({ service }: { service: Service }) {
  const [editOpen, setEditOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const del = useDeleteService();
  const health = useHealth();
  const status = health.data?.[service.id];
  const menu = useContextMenu();

  return (
    <div
      className="service-card relative flex h-full flex-col rounded-lg border border-border bg-bg-card p-3 shadow-md shadow-black/30"
      onContextMenu={menu.onContextMenu}
    >
      {/* Permanent drag handle — top right. RGL's draggableHandle picks
          this up; the rest of the card body is not draggable so the user
          can right-click anywhere on the card without dragging it. */}
      <div
        className="rgl-drag-handle absolute right-2 top-2 z-10 cursor-grab rounded p-1 text-fg-muted/60 hover:bg-bg-elevated hover:text-fg active:cursor-grabbing"
        title="Drag to reorder"
        onContextMenu={(e) => e.stopPropagation()}
      >
        <GripIcon width={13} height={13} />
      </div>

      {/* Header */}
      <div className="flex items-center gap-2.5 pr-8">
        <Avatar name={service.name} icon={service.icon_path} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold leading-tight">
            {service.name}
          </div>
          {service.description && (
            <div className="mt-0.5 truncate text-[11px] text-fg-muted">
              {service.description}
            </div>
          )}
        </div>
      </div>

      {/* Hosts — not full-width, sized to fit content with a max cap. */}
      <div className="mt-2.5 flex flex-col items-start gap-1.5">
        <HostRow
          href={hostHref(service.host_primary, service.port_primary)}
          display={hostDisplay(service.host_primary, service.port_primary)}
          protocol={protocolLabel(service.host_primary)}
          status={status?.primary}
        />
        {service.host_alt && (
          <HostRow
            href={hostHref(service.host_alt, service.port_alt)}
            display={hostDisplay(service.host_alt, service.port_alt)}
            protocol={protocolLabel(service.host_alt)}
            status={status?.alt}
            secondary
          />
        )}
      </div>

      {/* Intentional empty space at the bottom — reserved for upcoming
          per-card content (system metrics, custom widgets, etc.). */}
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
      className={`group/host inline-flex h-8 max-w-full items-stretch overflow-hidden rounded-md border bg-bg-elevated/60 text-fg transition-colors hover:border-accent ${
        secondary ? 'border-border/50' : 'border-border-strong/60'
      }`}
    >
      <span
        className={`flex shrink-0 items-center justify-center px-2 font-mono text-[10px] font-bold uppercase tracking-wider text-white ${statusColor(status)}`}
        title={`${statusLabel(status)} · ${protocol}`}
      >
        {protocol}
      </span>
      <span className="flex-1 truncate px-2.5 py-1 font-mono text-xs leading-relaxed">
        {display}
      </span>
      <span className="flex items-center pr-2">
        <ExternalLinkIcon
          width={11}
          height={11}
          className="text-fg-muted opacity-40 transition-opacity group-hover/host:opacity-100"
        />
      </span>
    </a>
  );
}
