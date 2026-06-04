'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { AuthShell } from '@/components/auth-shell';

interface TOTPSetup {
  secret: string;
  otpauth_url: string;
  qr_png_base64: string;
  recovery_codes: string[];
}

export default function AccountPage() {
  return <AuthShell><Account /></AuthShell>;
}

function Account() {
  const [enabled, setEnabled] = useState(false);
  const [setup, setSetup] = useState<TOTPSetup | null>(null);
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);

  const refresh = async () => {
    try {
      const r = await api<{ enabled: boolean }>('/api/account/totp');
      setEnabled(r.enabled);
    } catch { /* */ }
  };
  useEffect(() => { refresh(); }, []);

  const begin = async () => {
    setBusy(true);
    try {
      const s = await api<TOTPSetup>('/api/account/totp/begin', { method: 'POST' });
      setSetup(s);
    } catch (e) { alert('failed: ' + String(e)); }
    finally { setBusy(false); }
  };
  const confirm = async () => {
    setBusy(true);
    try {
      await api('/api/account/totp/confirm', { method: 'POST', body: { code } });
      setSetup(null); setCode('');
      refresh();
      alert('2FA enabled.');
    } catch (e: unknown) { alert((e as { body?: { error?: string } })?.body?.error ?? 'invalid code'); }
    finally { setBusy(false); }
  };
  const disable = async () => {
    if (!window.confirm('Disable 2FA?')) return;
    try {
      await api('/api/account/totp/disable', { method: 'POST' });
      refresh();
    } catch (e) { alert('failed: ' + String(e)); }
  };

  return (
    <div className="mx-auto max-w-2xl px-6 py-8">
      <h1 className="text-2xl font-semibold tracking-tight">Account</h1>

      <section className="card mt-6">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-zinc-500">Two-factor authentication (TOTP)</h2>
        {!enabled && !setup && (
          <div>
            <p className="mb-3 text-sm text-zinc-500">Scan a QR with Google Authenticator / 1Password / Authy. Required on every login.</p>
            <button onClick={begin} disabled={busy} className="btn-primary">Enable 2FA</button>
          </div>
        )}
        {setup && (
          <div className="space-y-4">
            <div className="flex flex-col items-center gap-3">
              <img src={`data:image/png;base64,${setup.qr_png_base64}`} alt="QR" className="rounded-md border border-zinc-200 dark:border-zinc-800" />
              <code className="font-mono text-[11px] text-zinc-500">{setup.secret}</code>
            </div>
            <div className="rounded-md bg-amber-50 p-3 text-xs text-amber-900 dark:bg-amber-900/30 dark:text-amber-200">
              <strong>Save these recovery codes</strong> — they are shown ONCE. Each can be used once if you lose your phone.
              <div className="mt-2 grid grid-cols-2 gap-1 font-mono text-[11px]">
                {setup.recovery_codes.map((c, i) => <div key={i}>{c}</div>)}
              </div>
            </div>
            <div className="flex items-end gap-2">
              <label className="block flex-1">
                <span className="text-xs text-zinc-500">Verify a 6-digit code to enable</span>
                <input className="input mt-1 font-mono" value={code} onChange={(e) => setCode(e.target.value)} maxLength={6} />
              </label>
              <button onClick={confirm} disabled={busy || code.length !== 6} className="btn-primary">Confirm</button>
              <button onClick={() => { setSetup(null); setCode(''); }} className="btn-ghost">Cancel</button>
            </div>
          </div>
        )}
        {enabled && (
          <div className="space-y-3">
            <p className="text-sm text-emerald-700 dark:text-emerald-300">2FA is enabled on this account.</p>
            <button onClick={disable} className="btn-danger">Disable 2FA</button>
          </div>
        )}
      </section>

      <section className="card mt-6">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-zinc-500">Change password</h2>
        <ChangePasswordForm />
      </section>
    </div>
  );
}

function ChangePasswordForm() {
  const [cur, setCur] = useState('');
  const [nw, setNw] = useState('');
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (nw.length < 12) { alert('≥12 chars'); return; }
    setBusy(true);
    try {
      await api('/api/account/password', { method: 'POST', body: { current: cur, new: nw } });
      setCur(''); setNw('');
      alert('Password changed.');
    } catch (e: unknown) { alert((e as { body?: { error?: string } })?.body?.error ?? 'failed'); }
    finally { setBusy(false); }
  };
  return (
    <div className="space-y-3 text-sm">
      <label className="block">
        <span className="text-xs text-zinc-500">Current password</span>
        <input className="input mt-1" type="password" value={cur} onChange={(e) => setCur(e.target.value)} autoComplete="current-password" />
      </label>
      <label className="block">
        <span className="text-xs text-zinc-500">New password (≥12 chars)</span>
        <input className="input mt-1" type="password" value={nw} onChange={(e) => setNw(e.target.value)} autoComplete="new-password" />
      </label>
      <button onClick={submit} disabled={busy} className="btn-primary">Change</button>
    </div>
  );
}
