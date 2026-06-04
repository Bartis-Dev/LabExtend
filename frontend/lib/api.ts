// Tiny typed wrapper around fetch(). Adds:
//   - same-origin defaults (Cookie automatic)
//   - X-CSRF-Token header on state-changing methods (token read from /api/me)
//   - JSON parse + typed error throwing
//
// TODO(phase 5): wire CSRF token cache + refresh on 401; expose useApi() hook.

export type ApiError = Error & {
  status: number;
  body?: unknown;
};

let csrfToken: string | null = null;

export function setCSRFToken(t: string | null) {
  csrfToken = t;
}

export function getCSRFToken(): string | null {
  return csrfToken;
}

export interface RequestOpts extends Omit<RequestInit, 'body'> {
  body?: unknown; // auto-JSON-encoded for non-FormData bodies
  query?: Record<string, string | number | boolean | undefined>;
}

function buildURL(path: string, query?: RequestOpts['query']): string {
  if (!query) return path;
  const usp = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === null) continue;
    usp.set(k, String(v));
  }
  const qs = usp.toString();
  return qs ? `${path}?${qs}` : path;
}

export async function api<T = unknown>(path: string, opts: RequestOpts = {}): Promise<T> {
  const { body, query, headers, method, ...rest } = opts;
  const m = (method ?? (body !== undefined ? 'POST' : 'GET')).toUpperCase();
  const isMutation = m !== 'GET' && m !== 'HEAD' && m !== 'OPTIONS';

  const h: Record<string, string> = { Accept: 'application/json', ...(headers as Record<string, string>) };
  let payload: BodyInit | undefined;

  if (body !== undefined) {
    if (body instanceof FormData) {
      payload = body;
    } else {
      payload = JSON.stringify(body);
      h['Content-Type'] = 'application/json';
    }
  }

  if (isMutation && csrfToken) {
    h['X-CSRF-Token'] = csrfToken;
  }

  const res = await fetch(buildURL(path, query), {
    ...rest,
    method: m,
    credentials: 'same-origin',
    headers: h,
    body: payload,
  });

  const ct = res.headers.get('content-type') ?? '';
  const data: unknown = ct.includes('application/json') ? await res.json() : await res.text();

  if (!res.ok) {
    const err = new Error(`API ${m} ${path} → ${res.status}`) as ApiError;
    err.status = res.status;
    err.body = data;
    throw err;
  }
  return data as T;
}
