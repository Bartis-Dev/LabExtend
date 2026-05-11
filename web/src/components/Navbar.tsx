import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '@/store/auth';
import { LogOutIcon, SettingsIcon } from './icons';

export function Navbar() {
  const logout = useAuth((s) => s.logout);
  const nav = useNavigate();

  const handleLogout = async () => {
    await logout();
    nav('/auth', { replace: true });
  };

  return (
    <nav className="flex h-14 items-center justify-between border-b border-border bg-bg-card px-6">
      <Link to="/" className="font-bold tracking-wide">
        LabExtend
      </Link>
      <div className="flex items-center gap-1">
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
