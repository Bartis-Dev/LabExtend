import { lazy, Suspense } from 'react';

// Monaco is heavy; only load it when the user opens the Custom CSS tab.
const Monaco = lazy(() => import('@monaco-editor/react'));

export function CustomCssEditor({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <div className="overflow-hidden rounded border border-border">
      <Suspense
        fallback={
          <div className="grid h-72 place-items-center text-fg-muted">Loading editor…</div>
        }
      >
        <Monaco
          height="380px"
          defaultLanguage="css"
          theme="vs-dark"
          value={value}
          onChange={(v) => onChange(v ?? '')}
          options={{
            minimap: { enabled: false },
            fontSize: 13,
            scrollBeyondLastLine: false,
            tabSize: 2,
            wordWrap: 'on',
          }}
        />
      </Suspense>
      <p className="border-t border-border bg-bg-elevated/30 px-3 py-2 text-xs text-fg-muted">
        Overrides land after the palette variables. Use this for selectors Tailwind doesn't
        cover, e.g. <code>.react-grid-item {'{ ... }'}</code>.
      </p>
    </div>
  );
}
