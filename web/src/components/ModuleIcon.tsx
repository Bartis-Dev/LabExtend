import * as lucide from 'lucide-react';
import { Box, type LucideProps } from 'lucide-react';

// Lucide exports each icon in PascalCase; module rows store icons as
// kebab-case strings (e.g. "layout-dashboard") so they're easy to enter
// in forms. Unknown names fall back to a generic Box icon.
function toPascal(name: string): string {
  return name
    .split('-')
    .filter(Boolean)
    .map((p) => p[0].toUpperCase() + p.slice(1))
    .join('');
}

export function ModuleIcon({
  name,
  className,
  ...rest
}: { name: string } & LucideProps) {
  const Comp = (lucide as Record<string, unknown>)[toPascal(name)] as
    | React.ComponentType<LucideProps>
    | undefined;
  const Resolved = Comp ?? Box;
  return <Resolved className={className} aria-hidden {...rest} />;
}
