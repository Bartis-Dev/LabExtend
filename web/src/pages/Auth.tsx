import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '@/store/auth';
import { ForgotPasswordModal } from '@/components/ForgotPasswordModal';
import { ApiError } from '@/api/client';

export default function Auth() {
  const needsSetup = useAuth((s) => s.needsSetup);
  const login = useAuth((s) => s.login);
  const setup = useAuth((s) => s.setup);
  const nav = useNavigate();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [passwordConfirm, setPasswordConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [forgotOpen, setForgotOpen] = useState(false);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      if (needsSetup) {
        await setup({ username, password, password_confirm: passwordConfirm });
      } else {
        await login({ username, password });
      }
      nav('/', { replace: true });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'request failed');
    } finally {
      setBusy(false);
    }
  };

  const isSetup = needsSetup === true;
  const title = isSetup ? 'Welcome — set up LabExtend' : 'Sign in';
  const cta = isSetup ? 'Create account' : 'Login';

  return (
    <div className="grid h-full place-items-center p-4">
      <div className="w-full max-w-md rounded-lg border border-border bg-bg-card p-8 shadow-xl">
        <h1 className="mb-6 text-center text-2xl font-bold">{title}</h1>

        <form onSubmit={onSubmit} className="space-y-4">
          <Field
            label="Username"
            type="text"
            value={username}
            onChange={setUsername}
            autoFocus
            autoComplete="username"
          />
          <Field
            label="Password"
            type="password"
            value={password}
            onChange={setPassword}
            autoComplete={isSetup ? 'new-password' : 'current-password'}
          />
          {isSetup && (
            <Field
              label="Repeat password"
              type="password"
              value={passwordConfirm}
              onChange={setPasswordConfirm}
              autoComplete="new-password"
            />
          )}
          {error && (
            <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
              {error}
            </div>
          )}
          <button
            type="submit"
            disabled={busy}
            className="w-full rounded bg-accent px-4 py-2 font-semibold text-white hover:bg-accent-hover disabled:opacity-50"
          >
            {busy ? '…' : cta}
          </button>
        </form>

        {!isSetup && (
          <div className="mt-4 text-center text-sm">
            <button
              onClick={() => setForgotOpen(true)}
              className="text-fg-muted hover:text-fg"
            >
              Forgot password?
            </button>
          </div>
        )}
      </div>
      <ForgotPasswordModal open={forgotOpen} onClose={() => setForgotOpen(false)} />
    </div>
  );
}

function Field({
  label,
  value,
  onChange,
  ...rest
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  type: string;
  autoFocus?: boolean;
  autoComplete?: string;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-sm text-fg-muted">{label}</span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        required
        className="w-full rounded border border-border bg-bg-elevated px-3 py-2 outline-none focus:border-accent"
        {...rest}
      />
    </label>
  );
}
