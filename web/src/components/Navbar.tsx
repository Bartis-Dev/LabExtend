import { Link, NavLink, useNavigate } from 'react-router-dom';
import { useAuth } from '@/store/auth';
import { useModules, useSettings } from '@/api/queries';
import { useFavCard } from '@/store/favCard';
import type { Module } from '@/api/types';
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
    <nav className="flex h-14 items-center justify-between border-b border-border bg-bg-card px-6">
      <Link to="/" className="flex shrink-0 items-baseline gap-2 leading-none">
        {customName ? (
          <>
            <span className="font-bold tracking-wide">{customName}</span>
            <span className="text-[10px] font-medium uppercase tracking-wider text-fg-muted/60">
              LabExtend
            </span>
          </>
        ) : (
          <span className="font-bold tracking-wide">LabExtend</span>
        )}
      </Link>

      <div className="flex flex-1 items-center justify-center gap-1 overflow-x-auto">
        {visible.map((m) => {
          const to = moduleHref(m);
          return (
            <NavLink
              key={m.id}
              to={to}
              end={to === '/'}
              className={({ isActive }) =>
                'relative flex items-center gap-2 rounded px-3 py-1.5 text-sm transition-colors ' +
                (isActive
                  ? 'text-fg after:absolute after:inset-x-2 after:-bottom-[15px] after:h-[2px] after:bg-accent'
                  : 'text-fg-muted hover:text-fg')
              }
            >
              <ModuleIcon name={m.icon} className="h-4 w-4" />
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
              'rounded p-2 hover:bg-bg-elevated ' +
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
          className="rounded p-2 hover:bg-bg-elevated"
          aria-label="Settings"
        >
          <SettingsIcon />
        </Link>
        <button
          onClick={handleLogout}
          className="rounded p-2 hover:bg-bg-elevated"
          aria-label="Logout"
        >
          <LogOutIcon />
        </button>
      </div>
    </nav>
  );
}
