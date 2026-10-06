// Share's end-to-end encryption formats (docs/e2ee-plan.md, contract/crypto): sealed keys,
// locks, password locks, thumbnails and the recovery code. File contents are in content.ts.
import { b64u, concat, fromB64u, randomBytes, u32, utf8, type Bytes } from './bytes';
import { ecdh, open, seal, SealError, type Ephemeral } from './hpke';

export { SealError };

export const purposes = {
  file: 'share-e2ee-v1/file',
  folder: 'share-e2ee-v1/folder',
  person: 'share-e2ee-v1/person',
  content: 'share-e2ee-v1/content',
  thumb: 'share-e2ee-v1/thumb',
  invite: 'share-e2ee-v1/invite',
  pin: 'share-e2ee-v1/pin',
  recovery: 'share-e2ee-v1/recovery',
  check: 'share-e2ee-v1/check',
  code: 'share-e2ee-v1/code',
} as const;

/** What a folder key, and a file key sealed for it, are bound to. */
export const folderContext = (folderId: string, version: number) => utf8(`folder:${folderId}:${version}`);
/** What a person's private key is bound to. */
export const personContext = (userId: string) => utf8(`person:${userId}`);
export const recoveryContext = utf8('recovery');

/** A key pair: the private key for WebCrypto, its public key as sent, and its 32-byte scalar
 * when it may be sealed for others (never for a device key). */
export interface KeyPair {
  privateKey: CryptoKey;
  publicKey: Bytes;
  raw: Bytes | null;
}

/** A new key pair; extractable when its private key is to be sealed for others. */
export async function generateKeyPair(extractable: boolean): Promise<KeyPair> {
  const pair = (await crypto.subtle.generateKey(ecdh, extractable, ['deriveBits'])) as CryptoKeyPair;
  const publicKey = new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey));
  if (!extractable) return { privateKey: pair.privateKey, publicKey, raw: null };
  const raw = fromB64u((await crypto.subtle.exportKey('jwk', pair.privateKey)).d!);
  // Opening needs no more than this; the scalar is what gets sealed for others.
  return { privateKey: await importPrivateKey(raw, publicKey), publicKey, raw };
}

/** A private key from its scalar and its public key, for opening only. */
export async function importPrivateKey(raw: Bytes, publicKey: Bytes): Promise<CryptoKey> {
  if (raw.length !== 32 || publicKey.length !== 65 || publicKey[0] !== 4) throw new SealError('not a P-256 key');
  const jwk = { kty: 'EC', crv: 'P-256', d: b64u(raw), x: b64u(publicKey.subarray(1, 33)), y: b64u(publicKey.subarray(33)) };
  try {
    return await crypto.subtle.importKey('jwk', jwk, ecdh, false, ['deriveBits']);
  } catch {
    throw new SealError('not a P-256 key');
  }
}

/** A key pair from a private key's scalar and its public key. */
export async function keyPairOf(raw: Bytes, publicKey: Bytes): Promise<KeyPair> {
  return { privateKey: await importPrivateKey(raw, publicKey), publicKey, raw };
}

/** Seals a key (32 bytes) for the holder of publicKey, for purpose, bound to aad. */
export function sealKey(publicKey: Bytes, purpose: string, aad: Bytes, key: Bytes, ephemeral?: Ephemeral): Promise<Bytes> {
  return seal(publicKey, utf8(purpose), aad, key, ephemeral);
}

/** Opens a sealed key with the key pair it was sealed for. */
export function openKey(pair: { privateKey: CryptoKey; publicKey: Bytes }, purpose: string, aad: Bytes, sealed: Bytes): Promise<Bytes> {
  return open(pair.privateKey, pair.publicKey, utf8(purpose), aad, sealed);
}

async function aesKey(key: Bytes): Promise<CryptoKey> {
  return crypto.subtle.importKey('raw', key, 'AES-GCM', false, ['encrypt', 'decrypt']);
}

/** Locks plaintext with a 32-byte key, bound to aad: a random nonce, then the ciphertext. */
export async function lock(key: Bytes, aad: Bytes, plaintext: Bytes): Promise<Bytes> {
  const nonce = randomBytes(12);
  const ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: nonce, additionalData: aad }, await aesKey(key), plaintext);
  return concat(nonce, new Uint8Array(ct));
}

export async function unlock(key: Bytes, aad: Bytes, locked: Bytes): Promise<Bytes> {
  if (locked.length < 28) throw new SealError('too short');
  try {
    return new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: locked.subarray(0, 12), additionalData: aad }, await aesKey(key), locked.subarray(12)));
  } catch {
    throw new SealError("can't be unlocked");
  }
}

/** HKDF-SHA256's 32-byte key from secret, with salt, for purpose. */
export async function hkdf(secret: Bytes, salt: Bytes, purpose: string): Promise<Bytes> {
  const k = await crypto.subtle.importKey('raw', secret, 'HKDF', false, ['deriveBits']);
  return new Uint8Array(await crypto.subtle.deriveBits({ name: 'HKDF', hash: 'SHA-256', salt, info: utf8(purpose) }, k, 256));
}

/** The key a link's secret or the recovery code locks with. */
export const secretKey = (secret: Bytes, purpose: string) => hkdf(secret, new Uint8Array(0), purpose);

export const passwordIterations = 600_000;

async function passwordKey(password: string, salt: Bytes, iterations: number): Promise<Bytes> {
  const k = await crypto.subtle.importKey('raw', utf8(password), 'PBKDF2', false, ['deriveBits']);
  return new Uint8Array(await crypto.subtle.deriveBits({ name: 'PBKDF2', hash: 'SHA-256', salt, iterations }, k, 256));
}

/** Locks plaintext with a password: a salt, the iterations, then the lock. */
export async function passwordLock(password: string, aad: Bytes, plaintext: Bytes, iterations = passwordIterations): Promise<Bytes> {
  const salt = randomBytes(16);
  return concat(salt, u32(iterations), await lock(await passwordKey(password, salt, iterations), aad, plaintext));
}

export async function passwordUnlock(password: string, aad: Bytes, locked: Bytes): Promise<Bytes> {
  if (locked.length < 16 + 4 + 28) throw new SealError('too short');
  const iterations = new DataView(locked.buffer, locked.byteOffset + 16, 4).getUint32(0);
  if (iterations < passwordIterations / 10 || iterations > passwordIterations * 10) throw new SealError('not a password lock');
  return unlock(await passwordKey(password, locked.subarray(0, 16), iterations), aad, locked.subarray(20));
}

/** The key a file's thumbnail is locked with. */
export const thumbKey = (fileKey: Bytes) => hkdf(fileKey, new Uint8Array(0), purposes.thumb);

export async function sealThumb(fileKey: Bytes, jpeg: Bytes): Promise<Bytes> {
  return lock(await thumbKey(fileKey), new Uint8Array(0), jpeg);
}

export async function openThumb(fileKey: Bytes, sealed: Bytes): Promise<Bytes> {
  return unlock(await thumbKey(fileKey), new Uint8Array(0), sealed);
}

// The recovery code: 20 random bytes as 32 characters of Crockford's base32, in groups of four.

const crockford = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

export function formatRecoveryCode(secret: Bytes): string {
  let out = '';
  let acc = 0;
  let bits = 0;
  let n = 0;
  for (const b of secret) {
    acc = ((acc << 8) | b) & 0xffff;
    bits += 8;
    while (bits >= 5) {
      if (n > 0 && n % 4 === 0) out += '-';
      out += crockford[(acc >> (bits - 5)) & 31];
      bits -= 5;
      n++;
    }
  }
  return out;
}

/** The secret of a recovery code as someone types it: small letters, O for 0, I and L for 1,
 * dashes and spaces anywhere. Null for anything else. */
export function parseRecoveryCode(code: string): Bytes | null {
  const symbols = code.toUpperCase().replace(/[-\s]/g, '').replace(/O/g, '0').replace(/[IL]/g, '1');
  if (symbols.length !== 32) return null;
  const out = new Uint8Array(20);
  let acc = 0;
  let bits = 0;
  let at = 0;
  for (const c of symbols) {
    const v = crockford.indexOf(c);
    if (v < 0) return null;
    acc = ((acc << 5) | v) & 0xffff;
    bits += 5;
    if (bits >= 8) {
      out[at++] = (acc >> (bits - 8)) & 0xff;
      bits -= 8;
    }
  }
  return out;
}

export function newRecoveryCode(): { secret: Bytes; code: string } {
  const secret = randomBytes(20);
  return { secret, code: formatRecoveryCode(secret) };
}

// A check before keys are passed on (contract/crypto/check.json): the asking device commits to
// a nonce, the waiting side answers with its own, then the nonce is revealed, and both screens
// show the code.

export const checkNonceSize = 32;

/** What the asking device sends before its nonce. */
export async function commitment(nonce: Bytes): Promise<Bytes> {
  return new Uint8Array(await crypto.subtle.digest('SHA-256', concat(utf8(purposes.check), nonce)));
}

/** The 6 digits both screens show, from both nonces and the public key that gets the keys. */
export async function checkCode(askerNonce: Bytes, answerNonce: Bytes, publicKey: Bytes): Promise<string> {
  const h = new Uint8Array(await crypto.subtle.digest('SHA-256', concat(utf8(purposes.code), askerNonce, answerNonce, publicKey)));
  return String(new DataView(h.buffer).getUint32(0) % 1_000_000).padStart(6, '0');
}
