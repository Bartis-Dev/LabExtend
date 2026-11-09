'use client';

import { useCallback, useEffect, useState } from 'react';
import { Check, Copy, Download } from 'lucide-react';
import { api } from '@/lib/api';
import { AuthShell } from '@/components/auth-shell';
import { Modal } from '@/components/modal';

interface TOTPSetup {
  secret: string;
  otpauth_url: string;
  qr_png_base64: string;
  recovery_codes: string[];
}

interface TOTPStatus {
  enabled: boolean;
  recovery_codes_remaining: number;
}

interface OIDCStatus {
  enabled: boolean;
  name: string;
  linked: boolean;
}

type Msg = { tone: 'success' | 'error'; text: string } | null;

function errText(e: unknown, fallback: string): string {
  return (e as { body?: { error?: string } })?.body?.error ?? fallback;
}

function Notice({ msg }: { msg: Msg }) {
  if (!msg) return null;
  return (
    <div
      role={msg.tone === 'error' ? 'alert' : 'status'}
      className={
        msg.tone === 'error'
          ? 'rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-900/30 dark:text-red-300'
          : 'rounded-md bg-emerald-50 px-3 py-2 text-sm text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
      }
    >
      {msg.text}
    </div>
  );
}

function PasswordField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <label className="block max-w-xs">
      <span className="text-xs text-zinc-500">Confirm your current password</span>
      <input className="input mt-1" type="password" value={value} onChange={(e) => onChange(e.target.value)} autoComplete="current-password" />
    </label>
  );
}

export default function AccountPage() {
  return <AuthShell><Account /></AuthShell>;
}

function Account() {
  return (
    <div className="mx-auto max-w-2xl px-6 py-8">
      <h1 className="text-2xl font-semibold tracking-tight">Account</h1>
      <TOTPSection />
      <OIDCSection />
      <section className="card mt-6">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-zinc-500">Change password</h2>
        <ChangePasswordForm />
      </section>
    </div>
  );
}

function TOTPSection() {
  const [status, setStatus] = useState<TOTPStatus | null>(null);
  const [setup, setSetup] = useState<TOTPSetup | null>(null);
  // Codes from a regeneration, shown until the user dismisses them.
  const [freshCodes, setFreshCodes] = useState<string[] | null>(null);
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<Msg>(null);
  const [confirming, setConfirming] = useState<'disable' | 'regenerate' | null>(null);
  // Enrolling, disabling and regenerating all re-authenticate; the server
  // rejects them without the current password.
  const [pw, setPw] = useState('');

  const refresh = async () => {
    try {
      setStatus(await api<TOTPStatus>('/api/account/totp'));
    } catch (e) { setMsg({ tone: 'error', text: errText(e, 'Could not load 2FA status.') }); }
  };
  useEffect(() => { refresh(); }, []);

  const closeConfirm = useCallback(() => setConfirming(null), []);

  const begin = async () => {
    setBusy(true); setMsg(null);
    try {
      const s = await api<TOTPSetup>('/api/account/totp/begin', { method: 'POST', body: { password: pw } });
      setSetup(s); setPw('');
    } catch (e) { setMsg({ tone: 'error', text: errText(e, 'Could not start 2FA setup.') }); }
    finally { setBusy(false); }
  };
  const confirm = async () => {
    setBusy(true); setMsg(null);
    try {
      await api('/api/account/totp/confirm', { method: 'POST', body: { code } });
      setSetup(null); setCode('');
      await refresh();
      setMsg({ tone: 'success', text: '2FA is now enabled.' });
    } catch (e) { setMsg({ tone: 'error', text: errText(e, 'Invalid code.') }); }
    finally { setBusy(false); }
  };
  const disable = async () => {
    setBusy(true); setMsg(null);
    try {
      await api('/api/account/totp/disable', { method: 'POST', body: { password: pw } });
      setPw(''); setFreshCodes(null);
      await refresh();
      setMsg({ tone: 'success', text: '2FA has been disabled.' });
    } catch (e) { setMsg({ tone: 'error', text: errText(e, 'Could not disable 2FA.') }); }
    finally { setBusy(false); setConfirming(null); }
  };
  const regenerate = async () => {
    setBusy(true); setMsg(null);
    try {
      const r = await api<{ recovery_codes: string[] }>('/api/account/totp/recovery-codes', { method: 'POST', body: { password: pw } });
      setPw(''); setFreshCodes(r.recovery_codes);
      await refresh();
      setMsg({ tone: 'success', text: 'New recovery codes created. Your old codes no longer work.' });
    } catch (e) { setMsg({ tone: 'error', text: errText(e, 'Could not regenerate recovery codes.') }); }
    finally { setBusy(false); setConfirming(null); }
  };

  const enabled = status?.enabled ?? false;
  const left = status?.recovery_codes_remaining ?? 0;

  return (
    <section className="card mt-6">
      <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-zinc-500">Two-factor authentication (TOTP)</h2>
      <div className="space-y-3">
        <Notice msg={msg} />
        {status && !enabled && !setup && (
          <>
            <p className="text-sm text-zinc-500">Optional. Scan a QR code with Google Authenticator, 1Password or Authy; the code is then asked for on every password login.</p>
            <PasswordField value={pw} onChange={setPw} />
            <button onClick={begin} disabled={busy || !pw} className="btn-primary">Enable 2FA</button>
          </>
        )}
        {setup && (
          <div className="space-y-4">
            <div className="flex flex-col items-center gap-3">
              <img src={`data:image/png;base64,${setup.qr_png_base64}`} alt="QR code for your authenticator app" className="rounded-md border border-zinc-200 dark:border-zinc-800" />
              <code className="font-mono text-[11px] text-zinc-500">{setup.secret}</code>
            </div>
            <RecoveryCodes codes={setup.recovery_codes} />
            <div className="flex items-end gap-2">
              <label className="block flex-1">
                <span className="text-xs text-zinc-500">Verify a 6-digit code to enable</span>
                <input className="input mt-1 font-mono" value={code} onChange={(e) => setCode(e.target.value)} maxLength={6} inputMode="numeric" autoComplete="one-time-code" />
              </label>
              <button onClick={confirm} disabled={busy || code.length !== 6} className="btn-primary">Confirm</button>
              <button onClick={() => { setSetup(null); setCode(''); setMsg(null); }} className="btn-ghost">Cancel</button>
            </div>
          </div>
        )}
        {enabled && (
          <>
            <p className="text-sm text-emerald-700 dark:text-emerald-300">2FA is enabled on this account.</p>
            <p className={left <= 2 ? 'text-sm font-medium text-amber-700 dark:text-amber-300' : 'text-sm text-zinc-500'}>
              {left} of 8 recovery codes left.
              {left <= 2 && ' Regenerate them soon, or you may be locked out if you lose your authenticator.'}
            </p>
            {freshCodes && (
              <div className="space-y-2">
                <RecoveryCodes codes={freshCodes} />
                <button onClick={() => setFreshCodes(null)} className="btn-ghost">I have saved them</button>
              </div>
            )}
            <PasswordField value={pw} onChange={setPw} />
            <div className="flex flex-wrap gap-2">
              <button onClick={() => setConfirming('regenerate')} disabled={busy || !pw} className="btn-ghost border border-zinc-300 dark:border-zinc-700">Regenerate recovery codes</button>
              <button onClick={() => setConfirming('disable')} disabled={busy || !pw} className="btn-danger">Disable 2FA</button>
            </div>
          </>
        )}
      </div>

      {confirming && (
        <Modal
          title={confirming === 'disable' ? 'Disable 2FA?' : 'Regenerate recovery codes?'}
          onClose={closeConfirm}
          footer={
            <>
              <button onClick={closeConfirm} className="btn-ghost">Cancel</button>
              <button
                onClick={confirming === 'disable' ? disable : regenerate}
                disabled={busy}
                className={confirming === 'disable' ? 'btn-danger' : 'btn-primary'}
              >
                {busy ? 'Working…' : confirming === 'disable' ? 'Disable 2FA' : 'Regenerate'}
              </button>
            </>
          }
        >
          <p className="text-sm text-zinc-600 dark:text-zinc-300">
            {confirming === 'disable'
              ? 'Password logins will no longer ask for a code. Your authenticator entry and recovery codes stop working.'
              : 'All current recovery codes, used or not, stop working immediately. You get 8 new ones to save.'}
          </p>
        </Modal>
      )}
    </section>
  );
}

function RecoveryCodes({ codes }: { codes: string[] }) {
  const [copied, setCopied] = useState(false);
  const [copyErr, setCopyErr] = useState(false);
  const text = codes.join('\n') + '\n';

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true); setCopyErr(false);
      setTimeout(() => setCopied(false), 2000);
    } catch { setCopyErr(true); }
  };
  const download = () => {
    const url = URL.createObjectURL(new Blob([`LabExtend recovery codes\n\n${text}`], { type: 'text/plain' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = 'labextend-recovery-codes.txt';
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="rounded-md bg-amber-50 p-3 text-xs text-amber-900 dark:bg-amber-900/30 dark:text-amber-200">
      <strong>Save these recovery codes.</strong> They are shown only once. Each one works once if you lose your authenticator.
      <div className="mt-2 grid grid-cols-2 gap-1 font-mono text-[11px]">
        {codes.map((c) => <div key={c}>{c}</div>)}
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button onClick={copy} className="btn-ghost h-8 border border-amber-300 px-2.5 text-xs dark:border-amber-700">
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
          {copied ? 'Copied' : 'Copy'}
        </button>
        <button onClick={download} className="btn-ghost h-8 border border-amber-300 px-2.5 text-xs dark:border-amber-700">
          <Download className="h-3.5 w-3.5" />
          Download .txt
        </button>
        {copyErr && <span className="text-red-700 dark:text-red-300">Copy failed. Select the codes and copy them by hand.</span>}
      </div>
    </div>
  );
}

function OIDCSection() {
  const [status, setStatus] = useState<OIDCStatus | null>(null);
  const [pw, setPw] = useState('');
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<Msg>(null);

  const refresh = async () => {
    try { setStatus(await api<OIDCStatus>('/api/account/oidc')); } catch { /* section stays hidden */ }
  };
  useEffect(() => {
    refresh();
    // The callback redirects back here with the outcome in the query string.
    const q = new URLSearchParams(window.location.search);
    if (q.get('oidc') === 'linked') setMsg({ tone: 'success', text: 'Connected. You can now sign in with it.' });
    const err = q.get('oidc_error');
    if (err) setMsg({ tone: 'error', text: err });
    if (q.has('oidc') || err) window.history.replaceState(null, '', window.location.pathname);
  }, []);

  if (!status?.enabled) return null;

  const connect = async () => {
    setBusy(true); setMsg(null);
    try {
      const r = await api<{ redirect_url: string }>('/api/account/oidc/link', { method: 'POST', body: { password: pw } });
      window.location.assign(r.redirect_url);
    } catch (e) {
      setMsg({ tone: 'error', text: errText(e, 'Could not start connecting.') });
      setBusy(false);
    }
  };
  const disconnect = async () => {
    setBusy(true); setMsg(null);
    try {
      await api('/api/account/oidc/unlink', { method: 'POST', body: { password: pw } });
      setPw('');
      await refresh();
      setMsg({ tone: 'success', text: `${status.name} has been disconnected.` });
    } catch (e) { setMsg({ tone: 'error', text: errText(e, 'Could not disconnect.') }); }
    finally { setBusy(false); }
  };

  return (
    <section className="card mt-6">
      <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-zinc-500">{status.name}</h2>
      <div className="space-y-3">
        <Notice msg={msg} />
        <p className="text-sm text-zinc-500">
          {status.linked
            ? `Connected. You can sign in with ${status.name}; that login skips the TOTP step because the passkey already is a second factor.`
            : `Not connected. Connect your ${status.name} account to sign in with a passkey.`}
        </p>
        <PasswordField value={pw} onChange={setPw} />
        {status.linked ? (
          <button onClick={disconnect} disabled={busy || !pw} className="btn-danger">Disconnect</button>
        ) : (
          <button onClick={connect} disabled={busy || !pw} className="btn-primary">Connect {status.name}</button>
        )}
      </div>
    </section>
  );
}

function ChangePasswordForm() {
  const [cur, setCur] = useState('');
  const [nw, setNw] = useState('');
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<Msg>(null);
  const submit = async () => {
    if (nw.length < 12) { setMsg({ tone: 'error', text: 'The new password needs at least 12 characters.' }); return; }
    setBusy(true); setMsg(null);
    try {
      await api('/api/account/password', { method: 'POST', body: { current: cur, new: nw } });
      setCur(''); setNw('');
      setMsg({ tone: 'success', text: 'Password changed. Your other sessions were signed out.' });
    } catch (e) { setMsg({ tone: 'error', text: errText(e, 'Could not change the password.') }); }
    finally { setBusy(false); }
  };
  return (
    <div className="space-y-3 text-sm">
      <Notice msg={msg} />
      <label className="block">
        <span className="text-xs text-zinc-500">Current password</span>
        <input className="input mt-1" type="password" value={cur} onChange={(e) => setCur(e.target.value)} autoComplete="current-password" />
      </label>
      <label className="block">
        <span className="text-xs text-zinc-500">New password (at least 12 characters)</span>
        <input className="input mt-1" type="password" value={nw} onChange={(e) => setNw(e.target.value)} autoComplete="new-password" />
      </label>
      <button onClick={submit} disabled={busy} className="btn-primary">Change</button>
    </div>
  );
}
