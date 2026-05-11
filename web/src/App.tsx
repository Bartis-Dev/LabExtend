import { useEffect } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useAuth } from '@/store/auth';
import { useTheme } from '@/store/theme';
import { ThemeStyle } from '@/components/ThemeStyle';
import { Layout } from '@/components/Layout';
import Auth from '@/pages/Auth';
import Dashboard from '@/pages/Dashboard';
import Settings from '@/pages/Settings';

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
  // If already logged in, send to dashboard.
  if (user && !needsSetup) return <Navigate to="/" replace />;
  return children;
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
