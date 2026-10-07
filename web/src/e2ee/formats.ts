// Share's end-to-end encryption formats (docs/e2ee-plan.md, contract/crypto): sealed keys,
// locks, password locks, signatures, thumbnails, the recovery code and checks. File contents are
// in content.ts.
import { b64u, concat, fromB64u, randomBytes, u32, utf8, type Bytes } from './bytes';
import { dh, ecdh, open, seal, SealError, type Ephemeral } from './hpke';

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
  root: 'share-e2ee-v1/root',
  note: 'share-e2ee-v1/note',
  pinSecret: 'share-e2ee-v1/pin-secret',
  check: 'share-e2ee-v1/check',
  code: 'share-e2ee-v1/code',
  confirm: 'share-e2ee-v1/confirm',
} as const;

/** What a folder key, and a file key sealed for it, are bound to. */
export const folderContext = (folderId: string, version: number) => utf8(`folder:${folderId}:${version}`);
/** What a person's private key is bound to. */
export const personContext = (userId: string) => utf8(`person:${userId}`);
export const recoveryContext = utf8('recovery');
/** What the root's private key is bound to when sealed for an admin, and its public key when
 * locked with an invite's or a PIN's link. */
export const rootContext = utf8('root');
/** What a person's note is bound to. */
export const noteContext = (userId: string) => utf8(`note:${userId}`);
/** What a check's confirmation is bound to. */
export const checkContext = (id: string) => utf8(`check:${id}`);

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

/** The key a person's note is locked with, from their private key. */
export const noteKey = (personRaw: Bytes) => secretKey(personRaw, purposes.note);

/** The key a PIN link's secret is locked with, from a version of the folder's private key: only
 * someone who holds it can make one. */
export const pinSecretKey = (folderRaw: Bytes) => secretKey(folderRaw, purposes.pinSecret);

// What the root, which is the recovery key, signs (contract/crypto/sign.json): ECDSA on P-256 with
// SHA-256, which WebCrypto gives as r‖s, Share's format.

const ecdsa = { name: 'ECDSA', namedCurve: 'P-256' } as const;
const ecdsaSHA256 = { name: 'ECDSA', hash: 'SHA-256' } as const;

/** A version of a folder's key. */
export const folderKeyMessage = (folder: string, version: number, publicKey: Bytes) =>
  concat(utf8('share-e2ee-v1/sign/folder-key'), folderContext(folder, version), publicKey);
/** A folder that sends plain: never encrypted (version 0) or switched off, under its name. */
export const plainMessage = (folder: string, version: number, name: string) =>
  concat(utf8('share-e2ee-v1/sign/plain'), folderContext(folder, version), utf8(`\n${name}`));
/** The key of a new recovery code, signed with the old one. */
export const rootMessage = (publicKey: Bytes) => concat(utf8('share-e2ee-v1/sign/root'), publicKey);

/** Signs message with a private key's scalar and its public key: 64 bytes, r and s. */
export async function sign(raw: Bytes, publicKey: Bytes, message: Bytes): Promise<Bytes> {
  const jwk = { kty: 'EC', crv: 'P-256', d: b64u(raw), x: b64u(publicKey.subarray(1, 33)), y: b64u(publicKey.subarray(33)) };
  const key = await crypto.subtle.importKey('jwk', jwk, ecdsa, false, ['sign']);
  return new Uint8Array(await crypto.subtle.sign(ecdsaSHA256, key, message));
}

/** Whether signature is publicKey's signature of message. */
export async function verify(publicKey: Bytes, message: Bytes, signature: Bytes): Promise<boolean> {
  if (signature.length !== 64) return false;
  try {
    const key = await crypto.subtle.importKey('raw', publicKey, ecdsa, false, ['verify']);
    return await crypto.subtle.verify(ecdsaSHA256, key, signature, message);
  } catch {
    return false;
  }
}

/** The root's fingerprint in a PIN's link: the first 16 bytes of SHA-256. */
export async function fingerprint(publicKey: Bytes): Promise<Bytes> {
  return new Uint8Array(await crypto.subtle.digest('SHA-256', publicKey)).subarray(0, 16);
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
// a one-time key, the waiting side answers with its own, then the asking device reveals its key,
// and both screens show the code. After Allow, the asking device hands on what only the waiting
// side opens, with a key from the secret the two one-time keys make.

/** A one-time key pair for a check; its private key never leaves the page. */
export async function oneTimeKey(): Promise<{ privateKey: CryptoKey; publicKey: Bytes }> {
  const pair = (await crypto.subtle.generateKey(ecdh, false, ['deriveBits'])) as CryptoKeyPair;
  return { privateKey: pair.privateKey, publicKey: new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey)) };
}

/** What the asking device sends before its one-time key. */
export async function commitment(key: Bytes): Promise<Bytes> {
  return new Uint8Array(await crypto.subtle.digest('SHA-256', concat(utf8(purposes.check), key)));
}

/** The 6 digits both screens show, from both one-time keys and the public key that gets the keys. */
export async function checkCode(askerKey: Bytes, answerKey: Bytes, publicKey: Bytes): Promise<string> {
  const h = new Uint8Array(await crypto.subtle.digest('SHA-256', concat(utf8(purposes.code), askerKey, answerKey, publicKey)));
  return String(new DataView(h.buffer).getUint32(0) % 1_000_000).padStart(6, '0');
}

/** The key the asking device's confirmation is locked with: from one side's one-time private key
 * and the other side's public key, the same on both sides. */
export async function confirmKey(privateKey: CryptoKey, otherKey: Bytes, askerKey: Bytes, answerKey: Bytes, publicKey: Bytes): Promise<Bytes> {
  const salt = new Uint8Array(await crypto.subtle.digest('SHA-256', concat(askerKey, answerKey, publicKey)));
  return hkdf(await dh(privateKey, otherKey), salt, purposes.confirm);
}
