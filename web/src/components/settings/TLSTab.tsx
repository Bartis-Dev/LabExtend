import { useRef, useState } from 'react';
import { ApiError } from '@/api/client';
import {
  useGenerateSelfSignedTLS,
  useResetTLSCert,
  useTLSState,
  useUploadTLSCert,
} from '@/api/queries';

export function TLSTab() {
  const state = useTLSState();
  const upload = useUploadTLSCert();
  const selfSign = useGenerateSelfSignedTLS();
  const reset = useResetTLSCert();
  const [err, setErr] = useState<string | null>(null);
  const [okMsg, setOkMsg] = useState<string | null>(null);

  const data = state.data;

  return (
    <div className="space-y-6">
      <p className="text-sm text-fg-muted">
        LabExtend serves HTTPS only. The server boots with an auto-generated
        self-signed certificate if you haven&apos;t installed one yet — replace
        it below with a certificate covering the hostname / IP you actually
        use, or upload your own (e.g. issued by your internal CA / Let&apos;s
        Encrypt). New certs are hot-swapped: the next TLS handshake picks them
        up without a restart.
      </p>

      <section>
        <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Status
        </h3>
        <div className="rounded-lg border border-border bg-bg-card/40 p-4 text-sm">
          {state.isPending ? (
            <div className="text-fg-muted">Loading…</div>
          ) : data ? (
            <>
              <StatusBadge data={data} />
              {data.loaded && (
                <dl className="mt-3 grid grid-cols-[120px_1fr] gap-y-1 font-mono text-xs">
                  <dt className="text-fg-muted">Source</dt>
                  <dd>{labelForSource(data.source)}</dd>
                  <dt className="text-fg-muted">Subject</dt>
                  <dd className="break-all">{data.subject}</dd>
                  <dt className="text-fg-muted">Issuer</dt>
                  <dd className="break-all">{data.issuer}</dd>
                  {data.dns_names && data.dns_names.length > 0 && (
                    <>
                      <dt className="text-fg-muted">DNS names</dt>
                      <dd className="break-all">{data.dns_names.join(', ')}</dd>
                    </>
                  )}
                  {data.ips && data.ips.length > 0 && (
                    <>
                      <dt className="text-fg-muted">IP SANs</dt>
                      <dd className="break-all">{data.ips.join(', ')}</dd>
                    </>
                  )}
                  <dt className="text-fg-muted">Valid from</dt>
                  <dd>{fmt(data.not_before)}</dd>
                  <dt className="text-fg-muted">Valid until</dt>
                  <dd>{fmt(data.not_after)}</dd>
                </dl>
              )}
            </>
          ) : (
            <div className="text-fg-muted">No status.</div>
          )}
        </div>
      </section>

      {err && (
        <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
          {err}
        </div>
      )}
      {okMsg && (
        <div className="rounded border border-success/40 bg-success/10 px-3 py-2 text-sm text-success">
          {okMsg}
        </div>
      )}

      <SelfSignSection
        busy={selfSign.isPending}
        onSubmit={async (hostnames, days) => {
          setErr(null);
          setOkMsg(null);
          try {
            await selfSign.mutateAsync({ hostnames, validity_days: days });
            setOkMsg('Self-signed certificate installed. Next HTTPS handshake will use it.');
          } catch (e) {
            setErr(e instanceof ApiError ? e.message : 'failed');
          }
        }}
      />

      <UploadSection
        busy={upload.isPending}
        onSubmit={async (certPEM, keyPEM) => {
          setErr(null);
          setOkMsg(null);
          try {
            await upload.mutateAsync({ cert_pem: certPEM, key_pem: keyPEM });
            setOkMsg('Certificate installed. Next HTTPS handshake will use it.');
          } catch (e) {
            setErr(e instanceof ApiError ? e.message : 'failed');
          }
        }}
      />

      <section>
        <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Reset
        </h3>
        <div className="rounded-lg border border-border bg-bg-card/40 p-4 text-sm">
          <p className="mb-3 text-fg-muted">
            Deletes the cert files in <code className="font-mono">data/tls/</code> and
            immediately replaces them with a fresh self-signed certificate covering
            <code className="font-mono"> localhost</code> +
            <code className="font-mono"> 127.0.0.1</code>. HTTPS keeps working
            throughout — there&apos;s never a moment with no cert.
            Env-pointed certs (<code className="font-mono">LABEXTEND_TLS_CERT_FILE</code>)
            aren&apos;t touched.
          </p>
          <button
            onClick={async () => {
              if (!confirm('Reset to a fresh auto-generated self-signed certificate?')) return;
              setErr(null);
              setOkMsg(null);
              try {
                await reset.mutateAsync();
                setOkMsg('Reset to a fresh self-signed certificate.');
              } catch (e) {
                setErr(e instanceof ApiError ? e.message : 'failed');
              }
            }}
            className="rounded border border-border px-3 py-2 text-sm hover:bg-bg-elevated"
          >
            Reset to default self-signed
          </button>
        </div>
      </section>
    </div>
  );
}

function StatusBadge({ data }: { data: NonNullable<ReturnType<typeof useTLSState>['data']> }) {
  return (
    <div className="flex items-center gap-2">
      <span className="inline-block h-2.5 w-2.5 rounded-full bg-success" />
      <span className="text-sm">
        HTTPS active on <code className="font-mono">{data.listen}</code>.
      </span>
    </div>
  );
}

function SelfSignSection({
  onSubmit,
  busy,
}: {
  onSubmit: (hostnames: string[], days: number) => Promise<void>;
  busy: boolean;
}) {
  const [hostsRaw, setHostsRaw] = useState('localhost, 127.0.0.1');
  const [days, setDays] = useState('365');

  return (
    <section>
      <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-fg-muted">
        Generate self-signed
      </h3>
      <div className="space-y-3 rounded-lg border border-border bg-bg-card/40 p-4">
        <p className="text-sm text-fg-muted">
          Quick way to get HTTPS going on a home network. Browsers will warn about an unknown
          authority — that&apos;s expected for self-signed certificates. List the hostnames /
          IPs you&apos;ll use to reach LabExtend (comma- or space-separated).
        </p>
        <label className="block">
          <span className="mb-1 block text-xs text-fg-muted">Hostnames / IPs</span>
          <input
            value={hostsRaw}
            onChange={(e) => setHostsRaw(e.target.value)}
            placeholder="labextend.lan, 192.168.1.10"
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 font-mono text-sm outline-none focus:border-accent"
          />
        </label>
        <label className="block max-w-[160px]">
          <span className="mb-1 block text-xs text-fg-muted">Validity (days, 1–3650)</span>
          <input
            type="number"
            value={days}
            onChange={(e) => setDays(e.target.value)}
            min={1}
            max={3650}
            className="w-full rounded border border-border bg-bg-elevated px-3 py-2 text-sm outline-none focus:border-accent"
          />
        </label>
        <button
          onClick={async () => {
            const hostnames = hostsRaw
              .split(/[,\s]+/)
              .map((s) => s.trim())
              .filter(Boolean);
            const d = Math.max(1, Math.min(3650, Number(days) || 365));
            await onSubmit(hostnames, d);
          }}
          disabled={busy}
          className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
        >
          {busy ? 'Generating…' : 'Generate and install'}
        </button>
      </div>
    </section>
  );
}

function UploadSection({
  onSubmit,
  busy,
}: {
  onSubmit: (certPEM: string, keyPEM: string) => Promise<void>;
  busy: boolean;
}) {
  const [certPEM, setCertPEM] = useState('');
  const [keyPEM, setKeyPEM] = useState('');
  const certInput = useRef<HTMLInputElement | null>(null);
  const keyInput = useRef<HTMLInputElement | null>(null);

  const readFile = async (
    f: File | null | undefined,
    setter: (s: string) => void,
  ) => {
    if (!f) return;
    const text = await f.text();
    setter(text);
  };

  return (
    <section>
      <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-fg-muted">
        Upload your own
      </h3>
      <div className="space-y-3 rounded-lg border border-border bg-bg-card/40 p-4">
        <p className="text-sm text-fg-muted">
          PEM-encoded cert chain (full chain, leaf first) and private key. Both must match —
          the server validates before saving. Files are stored at{' '}
          <code className="font-mono">data/tls/cert.pem</code> and{' '}
          <code className="font-mono">data/tls/key.pem</code> with mode 0600.
        </p>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <div>
            <div className="mb-1 flex items-center justify-between">
              <span className="text-xs text-fg-muted">Certificate (PEM)</span>
              <input
                ref={certInput}
                type="file"
                accept=".pem,.crt,.cer,.cert,text/plain"
                onChange={(e) => readFile(e.target.files?.[0], setCertPEM)}
                className="hidden"
              />
              <button
                type="button"
                onClick={() => certInput.current?.click()}
                className="rounded border border-border px-2 py-0.5 text-[11px] hover:bg-bg-elevated"
              >
                Pick file…
              </button>
            </div>
            <textarea
              value={certPEM}
              onChange={(e) => setCertPEM(e.target.value)}
              rows={6}
              placeholder="-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----"
              className="w-full rounded border border-border bg-bg-elevated px-3 py-2 font-mono text-[11px] leading-tight outline-none focus:border-accent"
            />
          </div>
          <div>
            <div className="mb-1 flex items-center justify-between">
              <span className="text-xs text-fg-muted">Private key (PEM)</span>
              <input
                ref={keyInput}
                type="file"
                accept=".pem,.key,text/plain"
                onChange={(e) => readFile(e.target.files?.[0], setKeyPEM)}
                className="hidden"
              />
              <button
                type="button"
                onClick={() => keyInput.current?.click()}
                className="rounded border border-border px-2 py-0.5 text-[11px] hover:bg-bg-elevated"
              >
                Pick file…
              </button>
            </div>
            <textarea
              value={keyPEM}
              onChange={(e) => setKeyPEM(e.target.value)}
              rows={6}
              placeholder="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----"
              className="w-full rounded border border-border bg-bg-elevated px-3 py-2 font-mono text-[11px] leading-tight outline-none focus:border-accent"
            />
          </div>
        </div>
        <button
          onClick={() => onSubmit(certPEM, keyPEM)}
          disabled={busy || !certPEM.trim() || !keyPEM.trim()}
          className="rounded bg-accent px-4 py-2 text-sm text-white hover:bg-accent-hover disabled:opacity-50"
        >
          {busy ? 'Validating…' : 'Install'}
        </button>
      </div>
    </section>
  );
}

function labelForSource(src?: string): string {
  switch (src) {
    case 'env':
      return 'Environment variables (LABEXTEND_TLS_CERT_FILE / KEY_FILE)';
    case 'data_dir':
      return 'data/tls/ (uploaded or self-signed)';
    case 'self_signed':
      return 'Self-signed (generated locally)';
    default:
      return src ?? '—';
  }
}

function fmt(ts?: string): string {
  if (!ts) return '—';
  return new Date(ts).toLocaleString();
}
