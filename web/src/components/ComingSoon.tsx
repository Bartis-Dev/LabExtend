import { ModuleIcon } from './ModuleIcon';

export function ComingSoon({
  title,
  description,
  icon,
}: {
  title: string;
  description?: string;
  icon?: string;
}) {
  return (
    <div className="mx-auto flex max-w-xl flex-col items-center gap-4 p-12 text-center">
      <ModuleIcon name={icon ?? 'box'} className="h-12 w-12 text-fg-muted/60" />
      <h1 className="text-2xl font-bold">{title}</h1>
      <p className="text-fg-muted">
        {description ?? 'This module is coming in a future release.'}
      </p>
    </div>
  );
}
