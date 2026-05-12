import { useState } from 'react';
import { EditIcon, ExternalLinkIcon, GripIcon, TrashIcon } from './icons';
import { ConfirmDialog } from './Modal';
import { ServiceForm } from './ServiceForm';
import { ContextMenu, useContextMenu } from './ContextMenu';
import { useDeleteService, useHealth } from '@/api/queries';
import type { HostStatus, Service, ServiceStatus } from '@/api/types';

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

// Aggregate status across both hosts: 'up' when every monitored host is up,
// 'partial' when at least one is up and at least one is down, 'down' when
// every monitored host is down, 'na' when nothing is monitored at all.
function aggregateStatus(svc: Service, st: ServiceStatus | undefined): 'up' | 'down' | 'partial' | 'na' {
  const probes: HostStatus[] = [];
  const primaryMon = svc.ping_primary || svc.hc_primary_enabled;
  const altMon = !!svc.host_alt && (svc.ping_alt || svc.hc_alt_enabled);
  if (primaryMon) probes.push(st?.primary ?? 'n/a');
  if (altMon) probes.push(st?.alt ?? 'n/a');
  if (probes.length === 0) return 'na';
  const ups = probes.filter((s) => s === 'up').length;
  const downs = probes.filter((s) => s === 'down').length;
  if (ups === probes.length) return 'up';
  if (downs === probes.length) return 'down';
  return 'partial';
}

function bookmarkColor(s: 'up' | 'down' | 'partial' | 'na'): string {
  if (s === 'up') return 'bg-success';
  if (s === 'partial') return 'bg-warning';
  if (s === 'down') return 'bg-danger';
  return 'bg-fg-muted/40';
}

function bookmarkLabel(s: 'up' | 'down' | 'partial' | 'na'): string {
  if (s === 'up') return 'Online';
  if (s === 'partial') return 'Partial';
  if (s === 'down') return 'Offline';
  return 'N/A';
}

function iconUrl(p?: string | null): string | null {
  if (!p) return null;
  // Allow either a remote URL (icon picker) or a local upload path.
  if (/^https?:\/\//i.test(p)) return p;
  const name = p.startsWith('icons/') ? p.slice('icons/'.length) : p;
  return `/api/icons/${name}`;
}

function Avatar({ name, icon }: { name: string; icon?: string | null }) {
  const url = iconUrl(icon);
  if (!url) return null; // no fallback — empty header reads cleaner than a coloured letter block
  return (
    <img
      src={url}
      alt=""
      className="h-8 w-8 shrink-0 rounded-md border border-border/60 bg-bg-elevated object-cover"
      onError={(e) => ((e.currentTarget as HTMLImageElement).style.display = 'none')}
    />
  );
  // 'name' is intentionally unused here but kept for future fallback work.
  void name;
}

export function ServiceCard({ service }: { service: Service }) {
  const [editOpen, setEditOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [tooltipOpen, setTooltipOpen] = useState(false);
  const del = useDeleteService();
  const health = useHealth();
  const status = health.data?.[service.id];
  const menu = useContextMenu();
  const agg = aggregateStatus(service, status);

  return (
    <div
      className="service-card relative flex h-full flex-col rounded-lg border border-border bg-bg-card p-3.5 shadow-md shadow-black/30"
      onContextMenu={menu.onContextMenu}
    >
      {/* Status bookmark — hangs from the top edge, ~70% from left.
          Aggregates across both hosts. Hovering shows a per-host breakdown. */}
      {agg !== 'na' && (
        <div
          className="absolute -top-1.5 left-[68%] z-10"
          onMouseEnter={() => setTooltipOpen(true)}
          onMouseLeave={() => setTooltipOpen(false)}
        >
          <div
            className={`rounded-md px-2.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white shadow-md ${bookmarkColor(agg)}`}
          >
            {bookmarkLabel(agg)}
          </div>
          {tooltipOpen && (
            <div className="absolute right-0 top-full z-20 mt-2 min-w-[14rem] rounded-md border border-border bg-bg-card p-2.5 text-xs shadow-2xl">
              <StatusRow
                label="Primary"
                display={hostDisplay(service.host_primary, service.port_primary)}
                status={
                  service.ping_primary || service.hc_primary_enabled
                    ? (status?.primary ?? 'n/a')
                    : undefined
                }
              />
              {service.host_alt && (
                <StatusRow
                  label="Alt"
                  display={hostDisplay(service.host_alt, service.port_alt)}
                  status={
                    service.ping_alt || service.hc_alt_enabled
                      ? (status?.alt ?? 'n/a')
                      : undefined
                  }
                />
              )}
            </div>
          )}
        </div>
      )}

      {/* Drag handle — top right */}
      <div
        className="rgl-drag-handle absolute right-2 top-2 z-10 cursor-grab rounded p-1 text-fg-muted/60 hover:bg-bg-elevated hover:text-fg active:cursor-grabbing"
        title="Drag to reorder"
        onContextMenu={(e) => e.stopPropagation()}
      >
        <GripIcon width={13} height={13} />
      </div>

      {/* Header */}
      <div className="flex items-center gap-3 pr-8">
        <Avatar name={service.name} icon={service.icon_path} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold leading-tight">
            {service.name}
          </div>
          {service.description && (
            <div className="mt-1 truncate text-[11px] text-fg-muted">
              {service.description}
            </div>
          )}
        </div>
      </div>

      {/* Hosts — wider buttons (min-width forces them to fill more of the card), taller. */}
      <div className="mt-3.5 flex flex-col items-start gap-2">
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

function StatusRow({
  label,
  display,
  status,
}: {
  label: string;
  display: string;
  status: HostStatus | undefined;
}) {
  return (
    <div className="flex items-center justify-between gap-3 py-1">
      <div className="flex min-w-0 items-center gap-2">
        <span className={`h-2 w-2 shrink-0 rounded-full ${statusDotColor(status)}`} />
        <span className="text-fg-muted">{label}:</span>
        <span className="truncate font-mono text-fg">{display}</span>
      </div>
      <span className="shrink-0 text-fg-muted">{status ? statusLabel(status) : 'Off'}</span>
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
      {/* Status dot — small, on its own */}
      <span className="flex shrink-0 items-center pl-2.5">
        <span className={`h-2 w-2 rounded-full ${statusDotColor(status)}`} title={statusLabel(status)} />
      </span>
      {/* Protocol label — plain text, subtle background, no shouting */}
      <span className="flex shrink-0 items-center border-r border-border/40 px-2 font-mono text-[10px] font-medium uppercase tracking-wider text-fg-muted">
        {protocol}
      </span>
      <span className="flex-1 truncate px-3 py-1 font-mono text-xs leading-relaxed">
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
