import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Modal } from './Modal';

type Source = {
  id: 'dashboard' | 'simple';
  label: string;
  manifestUrl: string;
  // Map a file entry from the manifest to a usable icon name (or null to skip).
  parseName: (filePath: string) => string | null;
  // Build the CDN URL for a given name.
  iconUrl: (name: string) => string;
  // Tailwind class applied to the <img> tag — used to invert single-color
  // SVG icons so they read on a dark background.
  imgClass?: string;
};

const SOURCES: Record<Source['id'], Source> = {
  dashboard: {
    id: 'dashboard',
    label: 'Dashboard Icons',
    manifestUrl:
      'https://data.jsdelivr.com/v1/package/gh/walkxcode/dashboard-icons/flat?branch=main',
    parseName: (p) => {
      const m = p.match(/^\/png\/(.+)\.png$/);
      return m ? m[1] : null;
    },
    iconUrl: (n) => `https://cdn.jsdelivr.net/gh/walkxcode/dashboard-icons/png/${n}.png`,
  },
  simple: {
    id: 'simple',
    label: 'Simple Icons',
    manifestUrl: 'https://data.jsdelivr.com/v1/package/npm/simple-icons/flat',
    parseName: (p) => {
      const m = p.match(/^\/icons\/(.+)\.svg$/);
      return m ? m[1] : null;
    },
    iconUrl: (n) => `https://cdn.jsdelivr.net/npm/simple-icons/icons/${n}.svg`,
    imgClass: 'invert', // single-color brand SVGs default to black; invert for dark theme
  },
};

type ManifestResponse = { files: { name: string }[] };

function useIconManifest(src: Source, enabled: boolean) {
  return useQuery({
    queryKey: ['icons', src.id],
    queryFn: async () => {
      const res = await fetch(src.manifestUrl);
      if (!res.ok) throw new Error(`manifest ${src.id} ${res.status}`);
      return (await res.json()) as ManifestResponse;
    },
    enabled,
    staleTime: 24 * 60 * 60 * 1000,
    gcTime: 24 * 60 * 60 * 1000,
  });
}

export function IconPicker({
  open,
  onClose,
  onPick,
}: {
  open: boolean;
  onClose: () => void;
  onPick: (url: string) => void;
}) {
  const [tab, setTab] = useState<Source['id']>('dashboard');
  const [search, setSearch] = useState('');
  const src = SOURCES[tab];
  const query = useIconManifest(src, open);

  const allNames = useMemo(() => {
    if (!query.data?.files) return [];
    const seen = new Set<string>();
    const out: string[] = [];
    for (const f of query.data.files) {
      const n = src.parseName(f.name);
      if (n && !seen.has(n)) {
        seen.add(n);
        out.push(n);
      }
    }
    out.sort();
    return out;
  }, [query.data, src]);

  const filtered = useMemo(() => {
    const q = search.toLowerCase().trim();
    if (!q) return allNames.slice(0, 240);
    return allNames.filter((n) => n.toLowerCase().includes(q)).slice(0, 240);
  }, [allNames, search]);

  const pick = (name: string) => {
    onPick(src.iconUrl(name));
    onClose();
  };

  return (
    <Modal open={open} onClose={onClose} title="Choose an icon" size="lg">
      <div className="flex flex-col gap-3">
        <div className="flex items-center gap-1 border-b border-border">
          {(Object.values(SOURCES) as Source[]).map((s) => (
            <button
              key={s.id}
              type="button"
              onClick={() => setTab(s.id)}
              className={`px-3 py-2 text-sm font-medium transition-colors ${
                tab === s.id
                  ? 'border-b-2 border-accent text-fg'
                  : 'text-fg-muted hover:text-fg'
              }`}
            >
              {s.label}
            </button>
          ))}
        </div>

        <input
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={`Search ${src.label.toLowerCase()}…`}
          autoFocus
          className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
        />

        <div className="min-h-[24rem] max-h-[60vh] overflow-y-auto rounded border border-border bg-bg-elevated/30 p-2">
          {query.isLoading && (
            <div className="grid h-48 place-items-center text-sm text-fg-muted">
              Loading icon catalogue…
            </div>
          )}
          {query.isError && (
            <div className="grid h-48 place-items-center text-sm text-danger">
              Failed to load icon manifest. Check your network.
            </div>
          )}
          {!query.isLoading && !query.isError && filtered.length === 0 && (
            <div className="grid h-48 place-items-center text-sm text-fg-muted">
              No icons match “{search}”.
            </div>
          )}
          {!query.isLoading && filtered.length > 0 && (
            <div className="grid grid-cols-6 gap-2 sm:grid-cols-8 md:grid-cols-10">
              {filtered.map((name) => (
                <button
                  key={name}
                  type="button"
                  onClick={() => pick(name)}
                  className="group flex flex-col items-center gap-1 rounded-md border border-transparent p-2 transition-colors hover:border-border hover:bg-bg-card"
                  title={name}
                >
                  <img
                    src={src.iconUrl(name)}
                    alt={name}
                    loading="lazy"
                    className={`h-9 w-9 object-contain ${src.imgClass ?? ''}`}
                    onError={(e) =>
                      ((e.currentTarget as HTMLImageElement).style.opacity = '0.2')
                    }
                  />
                  <span className="w-full truncate text-center text-[10px] text-fg-muted group-hover:text-fg">
                    {name}
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>

        <p className="text-xs text-fg-muted">
          Icons are served via CDN. The selected icon URL is stored on the service,
          so the dashboard always pulls the latest version.
        </p>
      </div>
    </Modal>
  );
}
