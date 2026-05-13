import { Navigate, useParams } from 'react-router-dom';
import { useModules } from '@/api/queries';

export default function IframeModulePage() {
  const { slug } = useParams<{ slug: string }>();
  const { data, isPending } = useModules();
  if (isPending) {
    return <div className="grid h-full place-items-center text-fg-muted">…</div>;
  }
  const mod = (data ?? []).find(
    (m) => m.kind === 'iframe' && m.slug === slug && m.enabled,
  );
  if (!mod || !mod.url) return <Navigate to="/" replace />;
  return (
    <div className="h-[calc(100vh-3.5rem)] w-full">
      <iframe
        src={mod.url}
        title={mod.name}
        className="h-full w-full border-0"
        sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox"
        referrerPolicy="no-referrer"
      />
    </div>
  );
}
