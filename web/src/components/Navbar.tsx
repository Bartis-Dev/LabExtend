import { Link, NavLink, useNavigate } from 'react-router-dom';
import { useAuth } from '@/store/auth';
import { useModules, useSettings } from '@/api/queries';
import { useFavCard } from '@/store/favCard';
import type { Module } from '@/api/types';
import { Logo } from './Logo';
import { ModuleIcon } from './ModuleIcon';
import { LogOutIcon, SettingsIcon } from './icons';

function moduleHref(m: Module): string {
  if (m.kind === 'iframe') return `/iframe/${m.slug}`;
  if (m.builtin_key === 'dashboard') return '/';
  return `/${m.slug}`;
}

export function Navbar() {
  const logout = useAuth((s) => s.logout);
  const settings = useSettings();
  const modules = useModules();
  const favVisible = useFavCard((s) => s.visible);
  const toggleFav = useFavCard((s) => s.toggle);
  const nav = useNavigate();

  const customName = (settings.data?.dashboard_name ?? '').trim();
  const visible = (modules.data ?? [])
    .filter((m) => m.enabled)
    .sort((a, b) => a.position - b.position || a.id - b.id);
  const notesEnabled = (modules.data ?? []).find((m) => m.slug === 'notes')?.enabled === true;

  const handleLogout = async () => {
    await logout();
    nav('/auth', { replace: true });
  };

  return (
    <nav className="flex h-14 items-center gap-4 border-b border-border bg-bg-card/95 px-5 backdrop-blur supports-[backdrop-filter]:bg-bg-card/75">
      {/* Brand: logo + wordmark. Wordmark uses display mono and is
          slightly de-emphasised when a custom dashboard name is set. */}
      <Link
        to="/"
        className="group flex shrink-0 items-center gap-2.5 leading-none focus-visible:outline-none"
        aria-label="LabExtend home"
      >
        <Logo className="h-6 w-6 text-fg transition-transform group-hover:scale-105" />
        {customName ? (
          <div className="flex flex-col leading-none">
            <span className="font-mono text-sm font-semibold tracking-tight text-fg">
              {customName}
            </span>
            <span className="mt-0.5 font-mono text-[9px] uppercase tracking-[0.18em] text-fg-muted/60">
              LabExtend
            </span>
          </div>
        ) : (
          <span className="font-mono text-sm font-semibold tracking-tight">
            Lab<span className="text-accent">Extend</span>
          </span>
        )}
      </Link>

      <div className="h-6 w-px shrink-0 bg-border" aria-hidden />

      <div className="flex flex-1 items-center gap-0.5 overflow-x-auto">
        {visible.map((m) => {
          const to = moduleHref(m);
          return (
            <NavLink
              key={m.id}
              to={to}
              end={to === '/'}
              className={({ isActive }) =>
                'relative flex shrink-0 items-center gap-1.5 rounded-md px-3 py-1.5 text-[13px] font-medium ' +
                (isActive
                  ? 'text-fg after:absolute after:inset-x-3 after:-bottom-[15px] after:h-[2px] after:rounded-full after:bg-accent after:shadow-[0_0_8px_var(--accent)]'
                  : 'text-fg-muted hover:bg-bg-elevated hover:text-fg')
              }
            >
              <ModuleIcon name={m.icon} className="h-3.5 w-3.5" />
              <span>{m.name}</span>
            </NavLink>
          );
        })}
      </div>

      <div className="flex shrink-0 items-center gap-1">
        {notesEnabled && (
          <button
            onClick={toggleFav}
            className={
              'rounded-md p-2 hover:bg-bg-elevated ' +
              (favVisible ? 'text-warning' : 'text-fg-muted')
            }
            aria-label="Toggle favourites card"
            title={favVisible ? 'Hide favourites card' : 'Show favourites card'}
          >
            <ModuleIcon name={favVisible ? 'star' : 'star-off'} className="h-5 w-5" />
          </button>
        )}
        <Link
          to="/settings"
          className="rounded-md p-2 text-fg-muted hover:bg-bg-elevated hover:text-fg"
          aria-label="Settings"
        >
          <SettingsIcon />
        </Link>
        <button
          onClick={handleLogout}
          className="rounded-md p-2 text-fg-muted hover:bg-bg-elevated hover:text-fg"
          aria-label="Logout"
        >
          <LogOutIcon />
        </button>
      </div>
    </nav>
  );
}
