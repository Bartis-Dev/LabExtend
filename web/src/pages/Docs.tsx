import { useEffect, useMemo, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeSanitize from 'rehype-sanitize';
import { ApiError } from '@/api/client';
import {
  useCreateDoc,
  useDeleteDoc,
  useDocs,
  useUpdateDoc,
} from '@/api/queries';
import type { DocPage, DocPageInput } from '@/api/types';
import { ModuleIcon } from '@/components/ModuleIcon';

export default function Docs() {
  const docs = useDocs();
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [mode, setMode] = useState<'view' | 'edit' | 'create-page' | 'create-link'>('view');

  const pages = docs.data ?? [];
  const selected = pages.find((p) => p.id === selectedId) ?? pages[0];

  useEffect(() => {
    if (!selectedId && pages.length > 0 && pages[0]) {
      setSelectedId(pages[0].id);
    }
  }, [pages, selectedId]);

  return (
    <div className="mx-auto grid h-full max-w-7xl grid-cols-[260px_1fr] gap-0 overflow-hidden">
      <Sidebar
        pages={pages}
        loading={docs.isPending}
        selectedId={selected?.id ?? null}
        onSelect={(id) => {
          setSelectedId(id);
          setMode('view');
        }}
        onAddPage={() => setMode('create-page')}
        onAddLink={() => setMode('create-link')}
      />

      <main className="overflow-y-auto p-8">
        {mode === 'create-page' && (
          <Editor
            key="new-page"
            onCancel={() => setMode('view')}
            onSaved={(p) => {
              setSelectedId(p.id);
              setMode('view');
            }}
          />
        )}
        {mode === 'create-link' && (
          <LinkEditor
            key="new-link"
            onCancel={() => setMode('view')}
            onSaved={(p) => {
              setSelectedId(p.id);
              setMode('view');
            }}
          />
        )}
        {mode === 'edit' && selected && (
          <Editor
            key={`edit-${selected.id}`}
            page={selected}
            onCancel={() => setMode('view')}
            onSaved={() => setMode('view')}
          />
        )}
        {mode === 'view' && (
          <Viewer page={selected} onEdit={() => setMode('edit')} />
        )}
      </main>
    </div>
  );
}

// --- Sidebar --------------------------------------------------------------

function Sidebar({
  pages,
  loading,
  selectedId,
  onSelect,
  onAddPage,
  onAddLink,
}: {
  pages: DocPage[];
  loading: boolean;
  selectedId: number | null;
  onSelect: (id: number) => void;
  onAddPage: () => void;
  onAddLink: () => void;
}) {
  const grouped = useMemo(() => {
    const m: Record<string, DocPage[]> = {};
    for (const p of pages) (m[p.category] ??= []).push(p);
    return m;
  }, [pages]);
  const cats = Object.keys(grouped).sort();

  return (
    <aside className="flex h-full flex-col border-r border-border bg-bg-card/40">
      <div className="flex items-center gap-1 border-b border-border p-3">
        <button
          onClick={onAddPage}
          className="flex-1 rounded bg-accent px-3 py-1.5 text-xs text-white hover:bg-accent-hover"
        >
          + Page
        </button>
        <button
          onClick={onAddLink}
          className="flex-1 rounded border border-border px-3 py-1.5 text-xs hover:bg-bg-elevated"
        >
          + Link
        </button>
      </div>
      <div className="flex-1 overflow-y-auto p-2">
        {loading && <div className="px-2 py-1 text-sm text-fg-muted">Loading…</div>}
        {!loading && pages.length === 0 && (
          <div className="px-2 py-4 text-sm text-fg-muted">
            No docs yet. Click &ldquo;+ Page&rdquo; to start.
          </div>
        )}
        {cats.map((cat) => (
          <div key={cat} className="mb-3">
            <div className="mb-1 px-2 text-[10px] font-semibold uppercase tracking-wider text-fg-muted/80">
              {cat}
            </div>
            <ul className="space-y-0.5">
              {grouped[cat].map((p) => (
                <li key={p.id}>
                  {p.is_link ? (
                    <a
                      href={p.link_url ?? '#'}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex items-center gap-2 truncate rounded px-2 py-1 text-sm text-fg-muted hover:bg-bg-elevated hover:text-fg"
                    >
                      <ModuleIcon name="external-link" className="h-3.5 w-3.5 shrink-0" />
                      <span className="truncate">{p.title}</span>
                    </a>
                  ) : (
                    <button
                      onClick={() => onSelect(p.id)}
                      className={
                        'flex w-full items-center gap-2 truncate rounded px-2 py-1 text-left text-sm transition-colors ' +
                        (selectedId === p.id
                          ? 'bg-accent/10 text-fg'
                          : 'text-fg-muted hover:bg-bg-elevated hover:text-fg')
                      }
                    >
                      <ModuleIcon name="file-text" className="h-3.5 w-3.5 shrink-0" />
                      <span className="truncate">{p.title}</span>
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </aside>
  );
}

// --- Viewer ---------------------------------------------------------------

function Viewer({ page, onEdit }: { page: DocPage | undefined; onEdit: () => void }) {
  if (!page) {
    return (
      <div className="grid h-full place-items-center text-fg-muted">
        Pick a page from the left, or create a new one.
      </div>
    );
  }
  if (page.is_link) {
    return (
      <div className="grid h-full place-items-center">
        <div className="rounded-lg border border-border bg-bg-card/40 p-6 text-center">
          <ModuleIcon name="external-link" className="mx-auto mb-3 h-8 w-8 text-fg-muted" />
          <h2 className="mb-2 text-xl font-bold">{page.title}</h2>
          <a
            href={page.link_url ?? '#'}
            target="_blank"
            rel="noopener noreferrer"
            className="font-mono text-sm text-accent hover:underline"
          >
            {page.link_url}
          </a>
        </div>
      </div>
    );
  }
  return (
    <article className="prose-doc max-w-3xl">
      <header className="mb-6 flex items-baseline justify-between">
        <h1 className="text-3xl font-bold">{page.title}</h1>
        <button
          onClick={onEdit}
          className="rounded border border-border px-3 py-1.5 text-sm hover:bg-bg-elevated"
        >
          Edit
        </button>
      </header>
      {page.content_markdown.trim() ? (
        <div className="docs-markdown">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            rehypePlugins={[rehypeSanitize]}
          >
            {page.content_markdown}
          </ReactMarkdown>
        </div>
      ) : (
        <p className="text-fg-muted">Empty page. Click &ldquo;Edit&rdquo; to add content.</p>
      )}
    </article>
  );
}

// --- Editor (page) --------------------------------------------------------

function slugify(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64)
    .replace(/^-+|-+$/g, '');
}

function Editor({
  page,
  onCancel,
  onSaved,
}: {
  page?: DocPage;
  onCancel: () => void;
  onSaved: (p: DocPage) => void;
}) {
  const create = useCreateDoc();
  const update = useUpdateDoc();
  const del = useDeleteDoc();

  const [title, setTitle] = useState(page?.title ?? '');
  const [slug, setSlug] = useState(page?.slug ?? '');
  const [category, setCategory] = useState(page?.category ?? 'General');
  const [content, setContent] = useState(page?.content_markdown ?? '');
  const [preview, setPreview] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Keep slug in sync with title while creating a new page (until the
  // user manually edits the slug).
  const [slugManual, setSlugManual] = useState(!!page);
  useEffect(() => {
    if (!slugManual) setSlug(slugify(title));
  }, [title, slugManual]);

  const save = async () => {
    setErr(null);
    const input: DocPageInput = {
      title,
      slug,
      category,
      content_markdown: content,
      is_link: false,
      link_url: null,
      position: page?.position ?? 0,
    };
    try {
      const out = page
        ? await update.mutateAsync({ id: page.id, input })
        : await create.mutateAsync(input);
      onSaved(out);
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'save failed');
    }
  };

  const doDelete = async () => {
    if (!page) return;
    if (!confirm(`Delete "${page.title}"?`)) return;
    try {
      await del.mutateAsync(page.id);
      onCancel();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'delete failed');
    }
  };

  const busy = create.isPending || update.isPending || del.isPending;

  return (
    <div className="mx-auto max-w-4xl space-y-3">
      <header className="flex items-center gap-2">
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Page title"
          required
          autoFocus
          className="flex-1 rounded border border-border bg-bg-elevated px-3 py-2 text-xl font-bold outline-none focus:border-accent"
        />
        <button
          onClick={() => setPreview((v) => !v)}
          className="rounded border border-border px-3 py-2 text-sm hover:bg-bg-elevated"
        >
          {preview ? 'Edit' : 'Preview'}
        </button>
        <button
          onClick={save}
          disabled={busy}
          className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
        >
          {busy ? '…' : 'Save'}
        </button>
        <button
          onClick={onCancel}
          className="rounded border border-border px-3 py-2 text-sm hover:bg-bg-elevated"
        >
          Cancel
        </button>
      </header>

      <div className="grid grid-cols-[1fr_1fr] gap-3">
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Category</span>
          <input
            value={category}
            onChange={(e) => setCategory(e.target.value)}
            maxLength={64}
            className="w-full rounded border border-border bg-bg-elevated px-3 py-1.5 text-sm outline-none focus:border-accent"
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Slug (URL key, must be unique)</span>
          <input
            value={slug}
            onChange={(e) => {
              setSlug(e.target.value);
              setSlugManual(true);
            }}
            placeholder="my-doc"
            required
            className="w-full rounded border border-border bg-bg-elevated px-3 py-1.5 font-mono text-sm outline-none focus:border-accent"
          />
        </label>
      </div>

      {preview ? (
        <div className="min-h-[400px] rounded border border-border bg-bg-card/40 p-4 docs-markdown">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            rehypePlugins={[rehypeSanitize]}
          >
            {content || '*(nothing to preview yet)*'}
          </ReactMarkdown>
        </div>
      ) : (
        <textarea
          value={content}
          onChange={(e) => setContent(e.target.value)}
          placeholder="Write markdown here — # heading, **bold**, `code`, lists, tables, links…"
          rows={24}
          className="w-full rounded border border-border bg-bg-elevated p-3 font-mono text-sm outline-none focus:border-accent"
        />
      )}

      {err && (
        <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
          {err}
        </div>
      )}

      {page && (
        <div className="pt-2">
          <button
            onClick={doDelete}
            className="rounded border border-danger/40 px-3 py-2 text-sm text-danger hover:bg-danger/10"
          >
            Delete page
          </button>
        </div>
      )}
    </div>
  );
}

// --- Link editor ----------------------------------------------------------

function LinkEditor({
  onCancel,
  onSaved,
}: {
  onCancel: () => void;
  onSaved: (p: DocPage) => void;
}) {
  const create = useCreateDoc();
  const [title, setTitle] = useState('');
  const [url, setUrl] = useState('');
  const [category, setCategory] = useState('Links');
  const [err, setErr] = useState<string | null>(null);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    const input: DocPageInput = {
      title,
      slug: slugify(title),
      category,
      content_markdown: '',
      is_link: true,
      link_url: url,
      position: 0,
    };
    try {
      const out = await create.mutateAsync(input);
      onSaved(out);
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'save failed');
    }
  };

  return (
    <form onSubmit={save} className="mx-auto max-w-md space-y-3">
      <header className="mb-3 flex items-center justify-between">
        <h2 className="text-xl font-bold">New external link</h2>
        <button
          type="button"
          onClick={onCancel}
          className="rounded border border-border px-3 py-1.5 text-sm hover:bg-bg-elevated"
        >
          Cancel
        </button>
      </header>
      <label className="block">
        <span className="mb-1 block text-xs text-fg-muted">Title</span>
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          required
          autoFocus
          placeholder="Docker install docs"
          className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
        />
      </label>
      <label className="block">
        <span className="mb-1 block text-xs text-fg-muted">URL</span>
        <input
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          required
          placeholder="https://docs.docker.com/engine/install/"
          className="w-full rounded border border-border bg-bg-elevated px-3 py-2 font-mono text-sm outline-none focus:border-accent"
        />
      </label>
      <label className="block">
        <span className="mb-1 block text-xs text-fg-muted">Category</span>
        <input
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          maxLength={64}
          className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
        />
      </label>
      {err && (
        <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
          {err}
        </div>
      )}
      <button
        type="submit"
        disabled={create.isPending}
        className="w-full rounded bg-accent px-4 py-2 text-white hover:bg-accent-hover disabled:opacity-50"
      >
        {create.isPending ? '…' : 'Save'}
      </button>
    </form>
  );
}
