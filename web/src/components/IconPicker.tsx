import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Modal } from './Modal';

type Source = {
  id: 'dashboard' | 'simple';
  label: string;
  // Ordered list of manifest URLs — the picker tries each one until one
  // succeeds. Lets us tolerate jsdelivr blips by falling back to the
  // GitHub git-tree API.
  manifestUrls: string[];
  // Map a raw entry from the chosen manifest to a usable icon name (or
  // null to skip). Each loader is paired with a list of URL templates.
  parseFlatJsdelivr: (filePath: string) => string | null;
  parseGitTree: (path: string) => string | null;
  // Build the CDN URL for a given name.
  iconUrl: (name: string) => string;
  imgClass?: string;
};

const SOURCES: Record<Source['id'], Source> = {
  dashboard: {
    id: 'dashboard',
    label: 'Dashboard Icons',
    manifestUrls: [
      // homarr-labs is the current maintainer; walkxcode is kept as a
      // legacy fallback in case the user's network reaches the old path.
      'https://data.jsdelivr.com/v1/package/gh/homarr-labs/dashboard-icons/flat?branch=main',
      'https://api.github.com/repos/homarr-labs/dashboard-icons/git/trees/main?recursive=1',
      'https://data.jsdelivr.com/v1/package/gh/walkxcode/dashboard-icons/flat?branch=main',
    ],
    parseFlatJsdelivr: (p) => {
      const m = p.match(/^\/png\/(.+)\.png$/);
      return m ? m[1] : null;
    },
    parseGitTree: (p) => {
      const m = p.match(/^png\/(.+)\.png$/);
      return m ? m[1] : null;
    },
    iconUrl: (n) => `https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/png/${n}.png`,
  },
  simple: {
    id: 'simple',
    label: 'Simple Icons',
    manifestUrls: [
      'https://data.jsdelivr.com/v1/package/npm/simple-icons/flat',
      'https://api.github.com/repos/simple-icons/simple-icons/git/trees/develop?recursive=1',
    ],
    parseFlatJsdelivr: (p) => {
      const m = p.match(/^\/icons\/(.+)\.svg$/);
      return m ? m[1] : null;
    },
    parseGitTree: (p) => {
      const m = p.match(/^icons\/(.+)\.svg$/);
      return m ? m[1] : null;
    },
    iconUrl: (n) => `https://cdn.jsdelivr.net/npm/simple-icons/icons/${n}.svg`,
    imgClass: 'invert',
  },
};

type ManifestResult = {
  names: string[];
};

async function fetchManifest(src: Source): Promise<ManifestResult> {
  let lastErr: unknown = null;
  for (const url of src.manifestUrls) {
    try {
      const res = await fetch(url, {
        // No credentials — these are public CDN/API endpoints.
        credentials: 'omit',
      });
      if (!res.ok) {
        lastErr = new Error(`${url} → HTTP ${res.status}`);
        continue;
      }
      const data: unknown = await res.json();
      const names = parseManifest(data, src);
      if (names.length > 0) return { names };
      lastErr = new Error(`${url} → empty list`);
    } catch (e) {
      lastErr = e;
    }
  }
  throw lastErr ?? new Error('all manifest sources failed');
}

function parseManifest(data: unknown, src: Source): string[] {
  // jsdelivr legacy /flat shape: { files: [{ name: '/path' }] }
  if (data && typeof data === 'object' && 'files' in data) {
    const files = (data as { files?: Array<{ name?: string }> }).files ?? [];
    return collectNames(files.map((f) => f.name ?? ''), src.parseFlatJsdelivr);
  }
  // GitHub git/trees shape: { tree: [{ path: 'png/plex.png', type: 'blob' }] }
  if (data && typeof data === 'object' && 'tree' in data) {
    const tree = (data as { tree?: Array<{ path?: string; type?: string }> }).tree ?? [];
    return collectNames(
      tree.filter((t) => t.type === 'blob').map((t) => t.path ?? ''),
      src.parseGitTree,
    );
  }
  return [];
}

function collectNames(entries: string[], parse: (s: string) => string | null): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const e of entries) {
    const n = parse(e);
    if (n && !seen.has(n)) {
      seen.add(n);
      out.push(n);
    }
  }
  out.sort();
  return out;
}

function useIconManifest(src: Source, enabled: boolean) {
  return useQuery({
    queryKey: ['icons', src.id],
    queryFn: () => fetchManifest(src),
    enabled,
    staleTime: 24 * 60 * 60 * 1000,
    gcTime: 24 * 60 * 60 * 1000,
    retry: 1,
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

  const filtered = useMemo(() => {
    const all = query.data?.names ?? [];
    const q = search.toLowerCase().trim();
    if (!q) return all.slice(0, 240);
    return all.filter((n) => n.toLowerCase().includes(q)).slice(0, 240);
  }, [query.data, search]);

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
            <div className="grid h-48 place-items-center px-6 text-center text-sm text-danger">
              <div>
                Failed to load icon manifest.
                <br />
                <span className="text-xs text-fg-muted">
                  {String((query.error as Error)?.message ?? 'unknown error')}
                </span>
              </div>
            </div>
          )}
          {!query.isLoading && !query.isError && filtered.length === 0 && (
            <div className="grid h-48 place-items-center text-sm text-fg-muted">
              No icons match "{search}".
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
          Icons are served via the public jsDelivr CDN. The selected URL is stored
          on the service so the dashboard always pulls the latest version.
        </p>
      </div>
    </Modal>
  );
}
