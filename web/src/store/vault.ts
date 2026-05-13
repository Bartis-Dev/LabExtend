// Vault state lives entirely in memory. The CryptoKey is non-extractable
// (Web Crypto guarantees this) so it cannot be read out, only used for
// encrypt/decrypt operations. The auto-lock timer wipes it after the
// configured idle period.
import { create } from 'zustand';
import { api } from '@/api/client';
import {
  base64ToBytes,
  bytesToBase64,
  buildVerifier,
  checkVerifier,
  DEFAULT_KDF_PARAMS,
  deriveKey,
  newSalt,
  type KDFParams,
} from '@/lib/vaultCrypto';

type RemoteState = {
  initialized: boolean;
  kdf_salt?: string;
  kdf_params_json?: string;
  verifier_ciphertext?: string;
  verifier_nonce?: string;
};

export type VaultStatus = 'unknown' | 'uninitialized' | 'locked' | 'unlocked';

type State = {
  status: VaultStatus;
  key: CryptoKey | null;
  autoLockMs: number;
  remote: RemoteState | null;
  lastActivityAt: number;
  // timer handle (browser setTimeout id)
  timerHandle: ReturnType<typeof setTimeout> | null;

  loadState(): Promise<void>;
  setup(password: string): Promise<void>;
  unlock(password: string): Promise<boolean>;
  lock(): void;
  markActivity(): void;
  setAutoLockMs(ms: number): void;
};

export const useVault = create<State>((set, get) => ({
  status: 'unknown',
  key: null,
  autoLockMs: 5 * 60_000, // 5 min default; can be overridden from Settings
  remote: null,
  lastActivityAt: Date.now(),
  timerHandle: null,

  async loadState() {
    const remote = await api.get<RemoteState>('/api/vault/state');
    set({
      remote,
      status: remote.initialized ? 'locked' : 'uninitialized',
    });
  },

  async setup(password: string) {
    const salt = newSalt();
    const params = DEFAULT_KDF_PARAMS;
    const key = await deriveKey(password, salt, params);
    const verifier = await buildVerifier(key);
    await api.post('/api/vault/setup', {
      kdf_salt: bytesToBase64(salt),
      kdf_params_json: JSON.stringify(params),
      verifier_ciphertext: bytesToBase64(verifier.ciphertext),
      verifier_nonce: bytesToBase64(verifier.nonce),
    });
    set({
      status: 'unlocked',
      key,
      remote: {
        initialized: true,
        kdf_salt: bytesToBase64(salt),
        kdf_params_json: JSON.stringify(params),
        verifier_ciphertext: bytesToBase64(verifier.ciphertext),
        verifier_nonce: bytesToBase64(verifier.nonce),
      },
      lastActivityAt: Date.now(),
    });
    get().markActivity();
  },

  async unlock(password: string) {
    const remote = get().remote;
    if (!remote || !remote.initialized || !remote.kdf_salt || !remote.kdf_params_json) {
      throw new Error('vault not initialized');
    }
    const params = JSON.parse(remote.kdf_params_json) as KDFParams;
    const salt = base64ToBytes(remote.kdf_salt);
    const key = await deriveKey(password, salt, params);
    const ok = await checkVerifier(
      key,
      base64ToBytes(remote.verifier_ciphertext!),
      base64ToBytes(remote.verifier_nonce!),
    );
    if (!ok) return false;
    set({ status: 'unlocked', key, lastActivityAt: Date.now() });
    get().markActivity();
    return true;
  },

  lock() {
    const t = get().timerHandle;
    if (t) clearTimeout(t);
    set({ status: 'locked', key: null, timerHandle: null });
  },

  markActivity() {
    const prev = get().timerHandle;
    if (prev) clearTimeout(prev);
    if (get().status !== 'unlocked') return;
    const ms = get().autoLockMs;
    const t = setTimeout(() => get().lock(), ms);
    set({ lastActivityAt: Date.now(), timerHandle: t });
  },

  setAutoLockMs(ms: number) {
    set({ autoLockMs: ms });
    get().markActivity(); // reset timer with new duration
  },
}));
