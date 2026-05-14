import { useMemo } from 'react';
import { NavLink, Navigate, useParams } from 'react-router-dom';
import type { Category, Section } from '@/data/commandLab';
import { CATEGORIES, getCategory } from '@/data/commandLab';
import { ModuleIcon } from '@/components/ModuleIcon';

// Group categories by shell label for the sidebar.
function useGrouped(): { shell: string; categories: Category[] }[] {
  return useMemo(() => {
    const map = new Map<string, Category[]>();
    for (const c of CATEGORIES) {
      const arr = map.get(c.shell) ?? [];
      arr.push(c);
      map.set(c.shell, arr);
    }
    return Array.from(map.entries()).map(([shell, categories]) => ({ shell, categories }));
  }, []);
}

export default function CommandLab() {
  const { categoryId } = useParams<{ categoryId: string }>();
  const grouped = useGrouped();

  if (!categoryId) {
    const first = grouped[0]?.categories[0];
    if (!first) return null;
    return <Navigate to={`/command-lab/${first.id}`} replace />;
  }
  const category = getCategory(categoryId);
  if (!category) {
    const first = grouped[0]?.categories[0];
    if (!first) return null;
    return <Navigate to={`/command-lab/${first.id}`} replace />;
  }

  return (
    <div className="mx-auto grid h-full max-w-7xl grid-cols-[260px_1fr] gap-0 overflow-hidden">
      <aside className="flex h-full flex-col overflow-y-auto border-r border-border bg-bg-card/40 p-3">
        <div className="mb-3 px-2">
          <div className="text-xs font-semibold uppercase tracking-wider text-fg-muted">
            Command Lab
          </div>
          <div className="text-[11px] text-fg-muted/70">Quick reference docs</div>
        </div>
        {grouped.map((g) => (
          <div key={g.shell} className="mb-3">
            <div className="mb-1 px-2 text-[10px] font-semibold uppercase tracking-wider text-fg-muted/80">
              {g.shell}
            </div>
            <ul className="space-y-0.5">
              {g.categories.map((c) => (
                <li key={c.id}>
                  <NavLink
                    to={`/command-lab/${c.id}`}
                    className={({ isActive }) =>
                      'flex items-center gap-2 rounded px-2 py-1.5 text-sm transition-colors ' +
                      (isActive
                        ? 'bg-accent/10 text-fg'
                        : 'text-fg-muted hover:bg-bg-elevated hover:text-fg')
                    }
                  >
                    <ModuleIcon name={c.icon} className="h-4 w-4 shrink-0" />
                    <span className="truncate">{c.label}</span>
                  </NavLink>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </aside>

      <main className="overflow-y-auto p-8">
        <CategoryDoc category={category} />
      </main>
    </div>
  );
}

function CategoryDoc({ category }: { category: Category }) {
  return (
    <article className="mx-auto max-w-3xl">
      <header className="mb-8 flex items-center gap-3 border-b border-border pb-5">
        <div className="grid h-12 w-12 place-items-center rounded-lg border border-border bg-bg-elevated">
          <ModuleIcon name={category.icon} className="h-6 w-6 text-fg-muted" />
        </div>
        <div>
          <div className="text-[11px] font-semibold uppercase tracking-wider text-fg-muted">
            {category.shell}
          </div>
          <h1 className="text-3xl font-bold leading-tight">{category.label}</h1>
          {category.description && (
            <p className="mt-1 text-sm text-fg-muted">{category.description}</p>
          )}
        </div>
      </header>

      <div className="space-y-8">
        {category.sections.map((s) => (
          <SectionView key={s.id} section={s} />
        ))}
      </div>
    </article>
  );
}

function SectionView({ section }: { section: Section }) {
  return (
    <section>
      <h2 className="mb-1 text-xl font-bold">{section.title}</h2>
      {section.description && (
        <p className="mb-3 text-sm text-fg-muted">{section.description}</p>
      )}
      {section.examples && section.examples.length > 0 && (
        <ul className="space-y-2">
          {section.examples.map((ex, i) => (
            <li key={i}>
              <CodeRow command={ex.command} note={ex.note} />
            </li>
          ))}
        </ul>
      )}
      {section.paths && section.paths.length > 0 && (
        <div className="mt-4 rounded-lg border border-border bg-bg-card/40 p-3">
          <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-fg-muted">
            Important paths
          </div>
          <ul className="space-y-1.5">
            {section.paths.map((p) => (
              <li key={p.path} className="flex items-start gap-3 text-sm">
                <button
                  onClick={() => navigator.clipboard?.writeText(p.path)}
                  title="Copy path"
                  className="shrink-0 rounded border border-border bg-bg-elevated px-2 py-0.5 font-mono text-xs hover:border-accent"
                >
                  {p.path}
                </button>
                <span className="text-fg-muted">{p.description}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {section.tip && (
        <div className="mt-3 rounded-md border border-accent/30 bg-accent/5 px-3 py-2 text-xs text-fg">
          <span className="font-semibold text-accent">Tip · </span>
          {section.tip}
        </div>
      )}
      {section.warning && (
        <div className="mt-3 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-fg">
          <span className="font-semibold text-warning">⚠ Warning · </span>
          {section.warning}
        </div>
      )}
    </section>
  );
}

function CodeRow({ command, note }: { command: string; note?: string }) {
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-bg-card/40">
      <div className="flex items-stretch">
        <pre className="flex-1 overflow-x-auto whitespace-pre-wrap break-all px-3 py-2 font-mono text-sm text-fg">
          {command}
        </pre>
        <button
          onClick={() => navigator.clipboard?.writeText(command)}
          title="Copy"
          className="shrink-0 border-l border-border px-3 text-xs text-fg-muted hover:bg-bg-elevated hover:text-fg"
        >
          Copy
        </button>
      </div>
      {note && (
        <div className="border-t border-border bg-bg-elevated/30 px-3 py-1.5 text-xs text-fg-muted">
          {note}
        </div>
      )}
    </div>
  );
}
