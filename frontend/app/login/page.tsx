'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';

interface OIDCConfig {
  enabled: boolean;
  name: string;
  error?: string;
}

export default function LoginPage() {
  const [error, setError] = useState<string | null>(null);
  const [cfg, setCfg] = useState<OIDCConfig | null>(null);

  useEffect(() => {
    api<OIDCConfig>('/api/auth/oidc/config')
      .then(setCfg)
      .catch(() => setCfg({ enabled: false, name: '', error: 'The leader is not reachable right now. Reload in a moment.' }));
    // A failed OIDC callback lands here with the reason in the query string.
    const q = new URLSearchParams(window.location.search);
    const err = q.get('oidc_error');
    if (err) {
      setError(err);
      window.history.replaceState(null, '', window.location.pathname);
    }
  }, []);

  return (
    <main className="mx-auto flex min-h-screen max-w-sm flex-col justify-center px-6 py-16">
      <h1 className="page-title">Sign in</h1>
      <p className="page-sub">Access the LabExtend dashboard.</p>

      <div className="mt-6 space-y-3">
        {cfg === null ? (
          <div className="skeleton h-9 w-full rounded-md" aria-hidden />
        ) : cfg.enabled ? (
          // A plain link: the endpoint answers with a redirect to the provider.
          <a href="/api/auth/oidc/login" className="btn-primary w-full">
            Sign in with {cfg.name}
          </a>
        ) : (
          <div role="alert" className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-900/30 dark:text-amber-200">
            {cfg.error}
          </div>
        )}
        {error && (
          <div role="alert" className="alert-error">
            {error}
          </div>
        )}
      </div>
    </main>
  );
}
