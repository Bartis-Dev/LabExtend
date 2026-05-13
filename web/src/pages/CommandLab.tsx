import { useEffect, useMemo, useRef, useState } from 'react';
import { NavLink, Navigate, useParams } from 'react-router-dom';
import type { Argument, Command, Flag, Shell } from '@/data/commandLab';
import { SHELLS, getShell } from '@/data/commandLab';
import { ModuleIcon } from '@/components/ModuleIcon';

export default function CommandLab() {
  const { shell: shellId } = useParams<{ shell: string }>();
  if (!shellId) return <Navigate to={`/command-lab/${SHELLS[0].id}`} replace />;
  const shell = getShell(shellId);
  if (!shell) return <Navigate to={`/command-lab/${SHELLS[0].id}`} replace />;

  return (
    <div className="mx-auto max-w-7xl p-6">
      <header className="mb-4 flex items-center gap-3">
        <h1 className="text-2xl font-bold">Command Lab</h1>
      </header>

      <nav className="mb-6 flex flex-wrap gap-1 border-b border-border">
        {SHELLS.map((s) => (
          <NavLink
            key={s.id}
            to={`/command-lab/${s.id}`}
            className={({ isActive }) =>
              '-mb-px flex items-center gap-2 px-4 py-2 text-sm transition-colors ' +
              (isActive
                ? 'border-b-2 border-accent text-fg'
                : 'border-b-2 border-transparent text-fg-muted hover:text-fg')
            }
          >
            <ModuleIcon name={s.icon} className="h-4 w-4" />
            {s.label}
          </NavLink>
        ))}
      </nav>

      <ShellView shell={shell} />
    </div>
  );
}

function ShellView({ shell }: { shell: Shell }) {
  const [tab, setTab] = useState<'commands' | 'paths'>('commands');
  const [search, setSearch] = useState('');
  const [selectedId, setSelectedId] = useState<string>(shell.commands[0]?.id ?? '');

  // Reset selection when switching shell.
  const selected = shell.commands.find((c) => c.id === selectedId) ?? shell.commands[0];

  return (
    <div>
      <p className="mb-4 text-sm text-fg-muted">{shell.description}</p>

      <div className="mb-4 flex gap-1">
        <SubTab active={tab === 'commands'} onClick={() => setTab('commands')}>
          Commands
        </SubTab>
        <SubTab active={tab === 'paths'} onClick={() => setTab('paths')}>
          Important paths
        </SubTab>
      </div>

      {tab === 'commands' && (
        <div className="grid gap-6 lg:grid-cols-[260px_1fr]">
          <aside>
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search…"
              className="mb-3 w-full rounded border border-border bg-bg-elevated px-3 py-2 text-sm outline-none focus:border-accent"
            />
            <CommandList
              commands={shell.commands}
              search={search}
              selectedId={selected?.id ?? ''}
              onSelect={setSelectedId}
            />
          </aside>
          {selected ? <CommandBuilder key={selected.id} command={selected} /> : null}
        </div>
      )}

      {tab === 'paths' && <PathBrowser shell={shell} />}
    </div>
  );
}

function SubTab({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={
        'rounded px-3 py-1.5 text-sm transition-colors ' +
        (active ? 'bg-bg-elevated text-fg' : 'text-fg-muted hover:bg-bg-elevated/60')
      }
    >
      {children}
    </button>
  );
}

function CommandList({
  commands,
  search,
  selectedId,
  onSelect,
}: {
  commands: Command[];
  search: string;
  selectedId: string;
  onSelect: (id: string) => void;
}) {
  const grouped = useMemo(() => {
    const q = search.trim().toLowerCase();
    const filtered = q
      ? commands.filter(
          (c) =>
            c.name.toLowerCase().includes(q) ||
            c.description.toLowerCase().includes(q) ||
            c.category.toLowerCase().includes(q),
        )
      : commands;
    const out: Record<string, Command[]> = {};
    for (const c of filtered) {
      (out[c.category] ??= []).push(c);
    }
    return out;
  }, [commands, search]);

  const cats = Object.keys(grouped).sort();
  if (cats.length === 0) {
    return <div className="text-sm text-fg-muted">No matches.</div>;
  }

  return (
    <div className="space-y-4">
      {cats.map((cat) => (
        <div key={cat}>
          <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-fg-muted/80">
            {cat}
          </div>
          <div className="rounded-lg border border-border bg-bg-card/40 divide-y divide-border">
            {grouped[cat].map((c) => (
              <button
                key={c.id}
                onClick={() => onSelect(c.id)}
                title={c.description}
                className={
                  'flex w-full min-w-0 flex-col gap-0.5 px-3 py-2 text-left transition-colors ' +
                  (selectedId === c.id ? 'bg-accent/10 text-fg' : 'hover:bg-bg-card')
                }
              >
                <span className="block w-full truncate font-mono text-sm">{c.name}</span>
                <span className="block w-full truncate text-[11px] text-fg-muted">
                  {c.description}
                </span>
              </button>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

// --- Command builder ------------------------------------------------------

type State = {
  args: Record<string, string>;
  flags: Record<string, string | boolean | number>;
};

function defaultState(cmd: Command): State {
  const args: Record<string, string> = {};
  for (const a of cmd.args ?? []) args[a.key] = '';
  const flags: Record<string, string | boolean | number> = {};
  for (const f of cmd.flags ?? []) {
    if (f.default !== undefined) flags[f.key] = f.default;
    else if (f.type === 'bool') flags[f.key] = false;
    else flags[f.key] = '';
  }
  return { args, flags };
}

function CommandBuilder({ command }: { command: Command }) {
  const [state, setState] = useState<State>(() => defaultState(command));
  const [copied, setCopied] = useState(false);

  const assembled = useMemo(() => assemble(command, state), [command, state]);

  const setArg = (key: string, v: string) =>
    setState((s) => ({ ...s, args: { ...s.args, [key]: v } }));
  const setFlag = (key: string, v: string | boolean | number) =>
    setState((s) => ({ ...s, flags: { ...s.flags, [key]: v } }));

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(assembled);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard may be unavailable over plain http */
    }
  };

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-xl font-bold">{command.name}</h2>
        <p className="text-sm text-fg-muted">{command.description}</p>
      </div>

      {/* Live preview */}
      <div className="rounded-lg border border-border bg-bg-card/40 p-4">
        <div className="mb-2 flex items-center justify-between">
          <span className="text-[10px] uppercase tracking-wider text-fg-muted">Preview</span>
          <button
            onClick={copy}
            className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-elevated"
          >
            {copied ? 'Copied!' : 'Copy'}
          </button>
        </div>
        <pre className="overflow-x-auto whitespace-pre-wrap break-all font-mono text-sm">
          {assembled || <span className="text-fg-muted">(fill in required fields)</span>}
        </pre>
      </div>

      {/* Args */}
      {command.args && command.args.length > 0 && (
        <div>
          <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-fg-muted/80">
            Arguments
          </div>
          <div className="space-y-3 rounded-lg border border-border bg-bg-card/40 p-4">
            {command.args.map((a) => (
              <div key={a.key}>
                <div className="mb-1 flex items-baseline justify-between text-xs">
                  <span className="font-medium">
                    {a.name}
                    {a.required && <span className="ml-1 text-danger">*</span>}
                  </span>
                  <span className="text-fg-muted">{a.description}</span>
                </div>
                <ArgumentInput
                  arg={a}
                  value={state.args[a.key] ?? ''}
                  onChange={(v) => setArg(a.key, v)}
                />
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Flags */}
      {command.flags && command.flags.length > 0 && (
        <div>
          <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-fg-muted/80">
            Flags
          </div>
          <div className="space-y-2 rounded-lg border border-border bg-bg-card/40 p-4">
            {command.flags.map((f) => (
              <FlagControl
                key={f.key}
                flag={f}
                value={state.flags[f.key]}
                onChange={(v) => setFlag(f.key, v)}
              />
            ))}
          </div>
        </div>
      )}

      {/* Examples + notes */}
      {(command.examples?.length || command.notes) && (
        <div className="space-y-2">
          {command.examples?.length ? (
            <div>
              <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-fg-muted/80">
                Examples
              </div>
              <div className="space-y-1.5">
                {command.examples.map((ex, i) => (
                  <pre
                    key={i}
                    className="cursor-pointer overflow-x-auto rounded border border-border bg-bg-card/40 px-3 py-2 font-mono text-xs hover:border-accent"
                    onClick={() => navigator.clipboard?.writeText(ex)}
                    title="Click to copy"
                  >
                    {ex}
                  </pre>
                ))}
              </div>
            </div>
          ) : null}
          {command.notes && (
            <div className="rounded border border-warning/40 bg-warning/10 px-3 py-2 text-xs">
              {command.notes}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function FlagControl({
  flag,
  value,
  onChange,
}: {
  flag: Flag;
  value: string | boolean | number | undefined;
  onChange: (v: string | boolean | number) => void;
}) {
  if (flag.type === 'bool') {
    return (
      <label className="flex items-start gap-3 text-sm">
        <input
          type="checkbox"
          checked={!!value}
          onChange={(e) => onChange(e.target.checked)}
          className="mt-1"
        />
        <div className="flex-1">
          <div className="flex items-baseline gap-2">
            <span className="font-mono text-xs">{flag.flag}</span>
            <span className="text-xs text-fg-muted">{flag.description}</span>
          </div>
        </div>
      </label>
    );
  }
  if (flag.type === 'enum' && flag.options) {
    return (
      <div className="flex flex-col gap-1 text-sm sm:flex-row sm:items-start">
        <div className="sm:w-44 sm:pt-1.5">
          <span className="font-mono text-xs">{flag.flag}</span>
          <span className="ml-2 text-[10px] text-fg-muted">{flag.description}</span>
        </div>
        <select
          value={String(value ?? '')}
          onChange={(e) => onChange(e.target.value)}
          className="flex-1 rounded border border-border bg-bg-elevated px-3 py-1.5 text-sm outline-none focus:border-accent"
        >
          <option value="">— not set —</option>
          {flag.options.map((opt) => (
            <option key={opt} value={opt}>
              {opt}
            </option>
          ))}
        </select>
      </div>
    );
  }
  // string / number — autosize so long values wrap instead of overflowing.
  return (
    <div className="flex flex-col gap-1 text-sm sm:flex-row sm:items-start">
      <div className="sm:w-44 sm:pt-1.5">
        <span className="font-mono text-xs">{flag.flag}</span>
        <span className="ml-2 text-[10px] text-fg-muted">{flag.description}</span>
      </div>
      <AutoGrowInput
        value={String(value ?? '')}
        onChange={(v) => onChange(flag.type === 'number' ? Number(v) : v)}
        placeholder={flag.placeholder}
        inputMode={flag.type === 'number' ? 'numeric' : undefined}
        className="flex-1"
      />
    </div>
  );
}

// ---- Argument input + special widgets ------------------------------------

function ArgumentInput({
  arg,
  value,
  onChange,
}: {
  arg: Argument;
  value: string;
  onChange: (v: string) => void;
}) {
  if (arg.kind === 'permissions') {
    return <PermissionsInput value={value} onChange={onChange} />;
  }
  return (
    <AutoGrowInput
      value={value}
      onChange={onChange}
      placeholder={arg.placeholder}
    />
  );
}

// Textarea that grows downward as its content wraps. Standard <input>
// scrolls horizontally for long values; we want long paths/URLs to wrap.
function AutoGrowInput({
  value,
  onChange,
  placeholder,
  inputMode,
  className,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  inputMode?: 'text' | 'numeric';
  className?: string;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    // Reset before measuring so it can both grow and shrink.
    el.style.height = 'auto';
    el.style.height = `${el.scrollHeight}px`;
  }, [value]);
  return (
    <textarea
      ref={ref}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      inputMode={inputMode}
      rows={1}
      onKeyDown={(e) => {
        // Enter submits forms / inserts newlines depending on host; for
        // single-line-ish use, treat Enter as a no-op newline.
        if (e.key === 'Enter' && !e.shiftKey) {
          e.preventDefault();
        }
      }}
      className={
        'block w-full resize-none overflow-hidden break-all rounded border border-border bg-bg-elevated px-3 py-1.5 font-mono text-sm outline-none focus:border-accent ' +
        (className ?? '')
      }
    />
  );
}

// chmod-style permissions: 3×3 grid (r/w/x × user/group/other).
// Stores the resulting octal mode (e.g. "750") in the arg value.
const PERM_CLASSES: { key: 'u' | 'g' | 'o'; label: string }[] = [
  { key: 'u', label: 'User (owner)' },
  { key: 'g', label: 'Group' },
  { key: 'o', label: 'Other' },
];
const PERM_BITS: { key: 'r' | 'w' | 'x'; label: string; bit: number }[] = [
  { key: 'r', label: 'Read', bit: 4 },
  { key: 'w', label: 'Write', bit: 2 },
  { key: 'x', label: 'Execute', bit: 1 },
];

function octalFromState(state: Record<string, boolean>): string {
  return PERM_CLASSES.map((cls) =>
    PERM_BITS.reduce(
      (sum, b) => (state[`${cls.key}${b.key}`] ? sum + b.bit : sum),
      0,
    ).toString(),
  ).join('');
}

function stateFromOctal(s: string): Record<string, boolean> {
  // Pad to 3 chars, e.g. "55" → "055". Anything non-octal: zero everything.
  const m = /^[0-7]{1,3}$/.test(s) ? s.padStart(3, '0') : '000';
  const out: Record<string, boolean> = {};
  PERM_CLASSES.forEach((cls, i) => {
    const digit = Number(m[i]);
    PERM_BITS.forEach((b) => {
      out[`${cls.key}${b.key}`] = (digit & b.bit) !== 0;
    });
  });
  return out;
}

function PermissionsInput({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  // Drive the checkbox state purely from the stored octal so the parent
  // is the source of truth (re-renders, command switch, etc.).
  const bits = useMemo(() => stateFromOctal(value || '000'), [value]);
  const toggle = (k: string) => {
    const next = { ...bits, [k]: !bits[k] };
    onChange(octalFromState(next));
  };

  return (
    <div className="rounded border border-border bg-bg-elevated p-3">
      <div className="grid grid-cols-[120px_repeat(3,1fr)_70px] items-center gap-2 text-sm">
        <div></div>
        {PERM_BITS.map((b) => (
          <div key={b.key} className="text-center text-xs text-fg-muted">
            {b.label}
          </div>
        ))}
        <div className="text-right text-xs text-fg-muted">Octal</div>
        {PERM_CLASSES.map((cls) => {
          const digit = PERM_BITS.reduce(
            (sum, b) => (bits[`${cls.key}${b.key}`] ? sum + b.bit : sum),
            0,
          );
          return (
            <FragmentRow key={cls.key}>
              <div className="text-xs">{cls.label}</div>
              {PERM_BITS.map((b) => {
                const k = `${cls.key}${b.key}`;
                return (
                  <label key={k} className="grid h-8 cursor-pointer place-items-center rounded hover:bg-bg-card">
                    <input
                      type="checkbox"
                      checked={!!bits[k]}
                      onChange={() => toggle(k)}
                    />
                  </label>
                );
              })}
              <div className="text-right font-mono text-sm">{digit}</div>
            </FragmentRow>
          );
        })}
      </div>
      <div className="mt-3 flex items-baseline justify-between border-t border-border pt-2">
        <span className="text-[10px] uppercase tracking-wider text-fg-muted">Octal mode</span>
        <span className="font-mono text-lg">{value || '000'}</span>
      </div>
    </div>
  );
}

// React.Fragment doesn't accept className/key tricks for grid rows;
// use a contents-display div instead.
function FragmentRow({ children }: { children: React.ReactNode }) {
  return <div className="contents">{children}</div>;
}

// Assemble the command line from template + state. Unknown placeholders
// or empty arg values collapse to nothing; double-spaces are squeezed.
function assemble(command: Command, state: State): string {
  const flagParts: string[] = [];
  for (const f of command.flags ?? []) {
    const v = state.flags[f.key];
    if (f.type === 'bool') {
      if (v === true) flagParts.push(f.flag);
    } else if (v !== undefined && v !== null && String(v).trim() !== '') {
      const val = String(v).trim();
      // Quote args containing whitespace, but only for string-style flags
      // where the value is a content rather than an attached option.
      const quoted = /\s/.test(val) ? `"${val}"` : val;
      flagParts.push(`${f.flag} ${quoted}`);
    }
  }
  const flagsStr = flagParts.join(' ');
  const argMap: Record<string, string> = { flags: flagsStr };
  for (const a of command.args ?? []) {
    argMap[a.key] = (state.args[a.key] ?? '').trim();
  }
  const out = command.template.replace(/\{(\w+)\}/g, (_m, k: string) =>
    argMap[k] !== undefined ? argMap[k] : '',
  );
  return out.replace(/\s+/g, ' ').trim();
}

// --- Path browser --------------------------------------------------------

function PathBrowser({ shell }: { shell: Shell }) {
  const grouped = useMemo(() => {
    const out: Record<string, typeof shell.paths> = {};
    for (const p of shell.paths) (out[p.category] ??= []).push(p);
    return out;
  }, [shell.paths]);
  const cats = Object.keys(grouped).sort();

  if (cats.length === 0) {
    return <div className="text-sm text-fg-muted">No paths catalogued for this shell.</div>;
  }

  return (
    <div className="space-y-6">
      {cats.map((cat) => (
        <section key={cat}>
          <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-fg-muted">
            {cat}
          </h3>
          <div className="rounded-lg border border-border bg-bg-card/40 divide-y divide-border">
            {grouped[cat].map((p) => (
              <div key={p.path} className="flex items-start gap-4 px-4 py-3 text-sm">
                <button
                  onClick={() => navigator.clipboard?.writeText(p.path)}
                  title="Copy path"
                  className="shrink-0 rounded border border-border bg-bg-elevated px-2 py-1 font-mono text-xs hover:border-accent"
                >
                  {p.path}
                </button>
                <span className="text-fg-muted">{p.description}</span>
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}
