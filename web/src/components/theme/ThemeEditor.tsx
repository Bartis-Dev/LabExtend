import { useEffect, useMemo, useState } from 'react';
import { PaletteEditor, defaultPalette } from './PaletteEditor';
import { CustomCssEditor } from './CustomCssEditor';
import { ConfirmDialog } from '@/components/Modal';
import {
  useActivateTheme,
  useCreateTheme,
  useDeleteTheme,
  useThemes,
  useUpdateTheme,
} from '@/api/queries';
import { useTheme } from '@/store/theme';
import { ApiError } from '@/api/client';
import type { Palette, Theme } from '@/api/types';

type Tab = 'palette' | 'css';

export function ThemeEditor() {
  const themes = useThemes();
  const setActiveTheme = useTheme((s) => s.setActive);

  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [name, setName] = useState('');
  const [palette, setPalette] = useState<Palette>(defaultPalette());
  const [customCss, setCustomCss] = useState('');
  const [tab, setTab] = useState<Tab>('palette');
  const [error, setError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<Theme | null>(null);

  const create = useCreateTheme();
  const update = useUpdateTheme();
  const remove = useDeleteTheme();
  const activate = useActivateTheme();

  // When the list loads, default to the active theme.
  useEffect(() => {
    if (!themes.data || selectedID !== null) return;
    const active = themes.data.find((t) => t.is_active) ?? themes.data[0];
    if (active) loadTheme(active);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [themes.data]);

  // Live preview: any time palette/customCss change while editing, push
  // the values into the global theme store so the rest of the app rerenders.
  useEffect(() => {
    setActiveTheme(
      selectedID ?? 0,
      name || 'Preview',
      palette,
      customCss,
      false,
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [palette, customCss]);

  const selectedTheme = useMemo(
    () => themes.data?.find((t) => t.id === selectedID) ?? null,
    [themes.data, selectedID],
  );

  function loadTheme(t: Theme) {
    setSelectedID(t.id);
    setName(t.name);
    setPalette({ ...defaultPalette(), ...t.palette });
    setCustomCss(t.custom_css);
    setError(null);
  }

  function newTheme() {
    setSelectedID(null);
    setName('');
    setPalette(defaultPalette());
    setCustomCss('');
    setError(null);
  }

  async function save() {
    setError(null);
    if (!name.trim()) {
      setError('Theme name is required.');
      return;
    }
    try {
      // If a theme with this name already exists, treat Save as an overwrite.
      const existing = themes.data?.find((t) => t.name === name.trim());
      const target = existing ?? selectedTheme;
      if (target && (existing || target.id === selectedID)) {
        const saved = await update.mutateAsync({
          id: target.id,
          input: { name: name.trim(), palette, custom_css: customCss },
        });
        setSelectedID(saved.id);
      } else {
        const created = await create.mutateAsync({
          name: name.trim(),
          palette,
          custom_css: customCss,
        });
        setSelectedID(created.id);
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'save failed');
    }
  }

  async function activateSelected(id: number) {
    setError(null);
    try {
      const activated = await activate.mutateAsync(id);
      setActiveTheme(
        activated.id,
        activated.name,
        activated.palette,
        activated.custom_css,
        activated.is_default,
      );
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'activate failed');
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-end gap-3">
        <label className="flex-1">
          <span className="mb-1 block text-xs text-fg-muted">Theme name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="My theme"
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
          />
        </label>
        <button
          onClick={newTheme}
          className="rounded border border-border px-4 py-2 hover:bg-bg-elevated"
        >
          New
        </button>
        <button
          onClick={save}
          disabled={create.isPending || update.isPending}
          className="rounded bg-accent px-4 py-2 font-semibold text-white hover:bg-accent-hover disabled:opacity-50"
        >
          {selectedID && name === selectedTheme?.name ? 'Save' : 'Save as'}
        </button>
      </div>

      <div className="flex gap-2 border-b border-border">
        <TabButton active={tab === 'palette'} onClick={() => setTab('palette')}>
          Quick Edit
        </TabButton>
        <TabButton active={tab === 'css'} onClick={() => setTab('css')}>
          Custom CSS
        </TabButton>
      </div>

      {tab === 'palette' ? (
        <PaletteEditor palette={palette} onChange={setPalette} />
      ) : (
        <CustomCssEditor value={customCss} onChange={setCustomCss} />
      )}

      {error && (
        <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}

      <div className="rounded border border-border">
        <div className="flex items-center justify-between border-b border-border bg-bg-elevated/30 px-3 py-2">
          <span className="text-sm font-semibold">Saved themes</span>
          <span className="text-xs text-fg-muted">{themes.data?.length ?? 0} total</span>
        </div>
        <ul className="divide-y divide-border">
          {(themes.data ?? []).map((t) => (
            <li key={t.id} className="flex items-center justify-between px-3 py-2">
              <div className="flex items-center gap-2">
                <span className="font-medium">{t.name}</span>
                {t.is_active && (
                  <span className="rounded bg-accent/20 px-1.5 py-0.5 text-xs text-accent">
                    active
                  </span>
                )}
                {t.is_default && (
                  <span className="rounded border border-border px-1.5 py-0.5 text-xs text-fg-muted">
                    default
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => loadTheme(t)}
                  className="text-sm text-fg-muted hover:text-fg"
                >
                  Edit
                </button>
                {!t.is_active && (
                  <button
                    onClick={() => activateSelected(t.id)}
                    className="rounded bg-accent px-2 py-1 text-xs text-white hover:bg-accent-hover"
                  >
                    Activate
                  </button>
                )}
                {!t.is_default && (
                  <button
                    onClick={() => setConfirmDelete(t)}
                    className="text-sm text-fg-muted hover:text-danger"
                  >
                    Delete
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
      </div>

      <ConfirmDialog
        open={!!confirmDelete}
        title="Delete theme?"
        message={`Delete "${confirmDelete?.name ?? ''}"? If it was active, the default theme will become active.`}
        onCancel={() => setConfirmDelete(null)}
        onConfirm={async () => {
          if (confirmDelete) {
            try {
              await remove.mutateAsync(confirmDelete.id);
              if (selectedID === confirmDelete.id) newTheme();
            } catch (err) {
              setError(err instanceof ApiError ? err.message : 'delete failed');
            }
          }
          setConfirmDelete(null);
        }}
      />
    </div>
  );
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={`px-4 py-2 text-sm ${
        active
          ? 'border-b-2 border-accent font-semibold text-fg'
          : 'text-fg-muted hover:text-fg'
      }`}
    >
      {children}
    </button>
  );
}
