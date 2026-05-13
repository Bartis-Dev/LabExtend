// Vault crypto: client-side Argon2id KDF + AES-GCM-256.
//
// The backend never sees the master password or the derived key — every
// entry is encrypted in the browser before being POSTed. The verifier
// ciphertext is how we prove the user typed the right password on unlock
// without storing it anywhere.
import { argon2id } from 'hash-wasm';

export type KDFParams = {
  // hash-wasm's argon2id call mirrors libargon2 parameters.
  // memory in kibibytes, time = iterations, parallelism = threads.
  algorithm: 'argon2id';
  memory_kib: number;
  iterations: number;
  parallelism: number;
  hash_len: number;
};

export const DEFAULT_KDF_PARAMS: KDFParams = {
  algorithm: 'argon2id',
  memory_kib: 64 * 1024, // 64 MiB
  iterations: 3,
  parallelism: 1, // browsers don't expose threads; we use 1 for portability
  hash_len: 32,
};

const VERIFIER_PLAINTEXT = 'labextend-vault-ok';

export function bytesToBase64(b: Uint8Array): string {
  let s = '';
  for (let i = 0; i < b.length; i++) s += String.fromCharCode(b[i]);
  return btoa(s);
}

export function base64ToBytes(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function randomBytes(n: number): Uint8Array {
  const b = new Uint8Array(n);
  crypto.getRandomValues(b);
  return b;
}

export async function deriveKey(
  password: string,
  salt: Uint8Array,
  params: KDFParams,
): Promise<CryptoKey> {
  if (params.algorithm !== 'argon2id') {
    throw new Error(`unsupported KDF algorithm: ${params.algorithm}`);
  }
  const raw = await argon2id({
    password,
    salt,
    iterations: params.iterations,
    parallelism: params.parallelism,
    memorySize: params.memory_kib,
    hashLength: params.hash_len,
    outputType: 'binary',
  });
  // TS 5+ marks Uint8Array<ArrayBufferLike> as not assignable to BufferSource
  // because of the SharedArrayBuffer branch. Our buffers are never shared, so
  // copy into a fresh ArrayBuffer-backed Uint8Array to satisfy the checker.
  return crypto.subtle.importKey(
    'raw',
    asBufferSource(raw as Uint8Array),
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  );
}

function asBufferSource(b: Uint8Array): ArrayBuffer {
  const out = new ArrayBuffer(b.byteLength);
  new Uint8Array(out).set(b);
  return out;
}

export async function encryptBytes(
  key: CryptoKey,
  plaintext: Uint8Array,
): Promise<{ ciphertext: Uint8Array; nonce: Uint8Array }> {
  const nonce = randomBytes(12);
  const ct = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: asBufferSource(nonce) },
    key,
    asBufferSource(plaintext),
  );
  return { ciphertext: new Uint8Array(ct), nonce };
}

export async function decryptBytes(
  key: CryptoKey,
  ciphertext: Uint8Array,
  nonce: Uint8Array,
): Promise<Uint8Array> {
  const pt = await crypto.subtle.decrypt(
    { name: 'AES-GCM', iv: asBufferSource(nonce) },
    key,
    asBufferSource(ciphertext),
  );
  return new Uint8Array(pt);
}

export async function buildVerifier(
  key: CryptoKey,
): Promise<{ ciphertext: Uint8Array; nonce: Uint8Array }> {
  return encryptBytes(key, new TextEncoder().encode(VERIFIER_PLAINTEXT));
}

export async function checkVerifier(
  key: CryptoKey,
  ciphertext: Uint8Array,
  nonce: Uint8Array,
): Promise<boolean> {
  try {
    const pt = await decryptBytes(key, ciphertext, nonce);
    return new TextDecoder().decode(pt) === VERIFIER_PLAINTEXT;
  } catch {
    return false;
  }
}

export function newSalt(): Uint8Array {
  return randomBytes(16);
}

// ---- Entry payload --------------------------------------------------------

export type EntryPayload = {
  name: string;
  website_url?: string;
  username?: string;
  secret: string;
  notes?: string;
  totp_secret?: string;
};

export async function encryptEntry(
  key: CryptoKey,
  payload: EntryPayload,
): Promise<{ payload_ciphertext: string; payload_nonce: string }> {
  const json = new TextEncoder().encode(JSON.stringify(payload));
  const { ciphertext, nonce } = await encryptBytes(key, json);
  return {
    payload_ciphertext: bytesToBase64(ciphertext),
    payload_nonce: bytesToBase64(nonce),
  };
}

export async function decryptEntry(
  key: CryptoKey,
  ciphertextB64: string,
  nonceB64: string,
): Promise<EntryPayload> {
  const pt = await decryptBytes(
    key,
    base64ToBytes(ciphertextB64),
    base64ToBytes(nonceB64),
  );
  return JSON.parse(new TextDecoder().decode(pt)) as EntryPayload;
}
