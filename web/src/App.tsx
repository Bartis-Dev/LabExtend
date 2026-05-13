import { lazy, Suspense, useEffect } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useAuth } from '@/store/auth';
import { useTheme } from '@/store/theme';
import { ThemeStyle } from '@/components/ThemeStyle';
import { Layout } from '@/components/Layout';
import { HealthWatcher } from '@/components/HealthWatcher';
import { EnabledGuard } from '@/components/EnabledGuard';
import { FloatingFavCardHost } from '@/components/FloatingFavCard';
import Auth from '@/pages/Auth';
import Dashboard from '@/pages/Dashboard';
import Settings from '@/pages/Settings';

const DDNS = lazy(() => import('@/pages/DDNS'));
const CommandLab = lazy(() => import('@/pages/CommandLab'));
const WoL = lazy(() => import('@/pages/WoL'));
const SecretsPage = lazy(() => import('@/pages/Secrets'));
const Docs = lazy(() => import('@/pages/Docs'));
const Notes = lazy(() => import('@/pages/Notes'));
const Stats = lazy(() => import('@/pages/Stats'));
const IframeModulePage = lazy(() => import('@/pages/IframeModule'));

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

function RequireAuth({ children }: { children: JSX.Element }) {
  const { ready, needsSetup, user } = useAuth();
  if (!ready) return <div className="grid h-full place-items-center text-fg-muted">…</div>;
  if (needsSetup || !user) return <Navigate to="/auth" replace />;
  return children;
}

function AuthOnly({ children }: { children: JSX.Element }) {
  const { ready, user, needsSetup } = useAuth();
  if (!ready) return <div className="grid h-full place-items-center text-fg-muted">…</div>;
  if (user && !needsSetup) return <Navigate to="/" replace />;
  return children;
}

function ModuleRoute({
  slug,
  children,
}: {
  slug: string;
  children: JSX.Element;
}) {
  return (
    <RequireAuth>
      <Layout>
        <EnabledGuard slug={slug}>
          <Suspense
            fallback={<div className="grid h-full place-items-center text-fg-muted">…</div>}
          >
            {children}
          </Suspense>
        </EnabledGuard>
      </Layout>
    </RequireAuth>
  );
}

export default function App() {
  const bootstrap = useAuth((s) => s.bootstrap);
  const setThemeFromBootstrap = useTheme((s) => s.setFromBootstrap);

  useEffect(() => {
    bootstrap()
      .then((b) => {
        if (b?.active_theme) setThemeFromBootstrap(b.active_theme);
      })
      .catch(() => {
        /* surfaced inside Auth page when relevant */
      });
  }, [bootstrap, setThemeFromBootstrap]);

  return (
    <QueryClientProvider client={queryClient}>
      <ThemeStyle />
      <HealthWatcher />
      <FloatingFavCardHost />
      <BrowserRouter>
        <Routes>
          <Route
            path="/auth"
            element={
              <AuthOnly>
                <Auth />
              </AuthOnly>
            }
          />
          <Route
            path="/"
            element={
              <RequireAuth>
                <Layout>
                  <Dashboard />
                </Layout>
              </RequireAuth>
            }
          />
          <Route path="/ddns" element={<ModuleRoute slug="ddns"><DDNS /></ModuleRoute>} />
          <Route
            path="/command-lab"
            element={<Navigate to="/command-lab/linux" replace />}
          />
          <Route
            path="/command-lab/:shell"
            element={<ModuleRoute slug="command-lab"><CommandLab /></ModuleRoute>}
          />
          <Route path="/wol" element={<ModuleRoute slug="wol"><WoL /></ModuleRoute>} />
          <Route
            path="/secrets"
            element={<ModuleRoute slug="secrets"><SecretsPage /></ModuleRoute>}
          />
          <Route path="/docs" element={<ModuleRoute slug="docs"><Docs /></ModuleRoute>} />
          <Route path="/notes" element={<ModuleRoute slug="notes"><Notes /></ModuleRoute>} />
          <Route path="/stats" element={<ModuleRoute slug="stats"><Stats /></ModuleRoute>} />
          <Route
            path="/iframe/:slug"
            element={
              <RequireAuth>
                <Layout>
                  <Suspense
                    fallback={<div className="grid h-full place-items-center text-fg-muted">…</div>}
                  >
                    <IframeModulePage />
                  </Suspense>
                </Layout>
              </RequireAuth>
            }
          />
          <Route
            path="/settings"
            element={
              <RequireAuth>
                <Layout>
                  <Settings />
                </Layout>
              </RequireAuth>
            }
          />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
