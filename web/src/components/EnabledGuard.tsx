import { Navigate } from 'react-router-dom';
import { useModules } from '@/api/queries';

// EnabledGuard sits between the route and its page: if the matching
// module is disabled (or doesn't exist), redirect to the dashboard.
// While useModules is loading, render a minimal placeholder so the page
// doesn't flicker through a redirect on first paint.
export function EnabledGuard({
  slug,
  children,
}: {
  slug: string;
  children: JSX.Element;
}) {
  const { data, isPending } = useModules();
  if (isPending) {
    return <div className="grid h-full place-items-center text-fg-muted">…</div>;
  }
  const mod = (data ?? []).find((m) => m.slug === slug);
  if (!mod || !mod.enabled) return <Navigate to="/" replace />;
  return children;
}
