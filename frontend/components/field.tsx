'use client';

/**
 * Field is the one form row: label, control, optional hint, inline error.
 *
 * A single control is wrapped in <label> so clicking the text focuses it.
 * Pass `group` for composite controls (segmented buttons, pickers): a <label>
 * around several buttons would forward every click on the text to the first one.
 */
export function Field({
  label,
  hint,
  error,
  group,
  className,
  children,
}: {
  label: React.ReactNode;
  hint?: React.ReactNode;
  error?: string | null;
  group?: boolean;
  className?: string;
  children: React.ReactNode;
}) {
  const inner = (
    <>
      <span className="label">{label}</span>
      <div className="mt-1.5">{children}</div>
      {hint && !error && <p className="hint mt-1.5">{hint}</p>}
      {error && <p className="field-error mt-1.5" role="alert">{error}</p>}
    </>
  );
  if (group) {
    return <div role="group" className={`block ${className ?? ''}`}>{inner}</div>;
  }
  return <label className={`block ${className ?? ''}`}>{inner}</label>;
}

/** Applies the invalid border to .input when a field has an error. */
export const inputCls = (error?: string | null, extra = '') =>
  `input ${error ? 'input-invalid' : ''} ${extra}`.trim();
