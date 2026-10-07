// HPKE in base mode (RFC 9180), one-shot, for the one suite Share seals keys with:
// DHKEM(P-256, HKDF-SHA256), HKDF-SHA256, AES-256-GCM. WebCrypto has every piece: ECDH gives
// P-256's shared x-coordinate, which is HPKE's DH output, also from a private key that can't be
// exported, and HMAC makes HKDF's two halves, which HPKE uses apart.
import { concat, utf8, type Bytes } from './bytes';

const kemID = 0x0010;
const kdfID = 0x0001;
const aeadID = 0x0002;

function i2osp(n: number, len: number): Bytes {
  const out = new Uint8Array(len);
  for (let i = len - 1; i >= 0; i--, n >>>= 8) out[i] = n & 0xff;
  return out;
}

const kemSuite = concat(utf8('KEM'), i2osp(kemID, 2));
const hpkeSuite = concat(utf8('HPKE'), i2osp(kemID, 2), i2osp(kdfID, 2), i2osp(aeadID, 2));
const version = utf8('HPKE-v1');
const empty = new Uint8Array(0);

/** P-256 for ECDH, as WebCrypto names it. */
export const ecdh = { name: 'ECDH', namedCurve: 'P-256' } as const;

export class SealError extends Error {}

async function hmac(key: Bytes, data: Bytes): Promise<Bytes> {
  // An empty key is HMAC's 32 zero bytes, padded the same way; WebCrypto refuses empty keys.
  const k = await crypto.subtle.importKey('raw', key.length ? key : new Uint8Array(32), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
  return new Uint8Array(await crypto.subtle.sign('HMAC', k, data));
}

async function expand(prk: Bytes, info: Bytes, length: number): Promise<Bytes> {
  const out = new Uint8Array(length);
  let t = empty as Bytes;
  for (let i = 1, at = 0; at < length; i++) {
    t = await hmac(prk, concat(t, info, new Uint8Array([i])));
    out.set(t.subarray(0, length - at), at);
    at += t.length;
  }
  return out;
}

const labeledExtract = (suite: Bytes, salt: Bytes, label: string, ikm: Bytes) => hmac(salt, concat(version, suite, utf8(label), ikm));

const labeledExpand = (suite: Bytes, prk: Bytes, label: string, info: Bytes, length: number) =>
  expand(prk, concat(i2osp(length, 2), version, suite, utf8(label), info), length);

/** P-256's shared x-coordinate of a private key and another public key. */
export async function dh(privateKey: CryptoKey, publicKey: Bytes): Promise<Bytes> {
  let pub: CryptoKey;
  try {
    pub = await crypto.subtle.importKey('raw', publicKey, ecdh, false, []);
  } catch {
    throw new SealError('not a P-256 public key');
  }
  return new Uint8Array(await crypto.subtle.deriveBits({ name: 'ECDH', public: pub }, privateKey, 256));
}

/** The AEAD key and nonce for the first message, from the KEM's shared secret. */
async function schedule(dhOut: Bytes, enc: Bytes, recipient: Bytes, info: Bytes): Promise<{ key: CryptoKey; nonce: Bytes }> {
  const prk = await labeledExtract(kemSuite, empty, 'eae_prk', dhOut);
  const shared = await labeledExpand(kemSuite, prk, 'shared_secret', concat(enc, recipient), 32);
  const context = concat(new Uint8Array([0]), await labeledExtract(hpkeSuite, empty, 'psk_id_hash', empty), await labeledExtract(hpkeSuite, empty, 'info_hash', info));
  const secret = await labeledExtract(hpkeSuite, shared, 'secret', empty);
  const key = await labeledExpand(hpkeSuite, secret, 'key', context, 32);
  const nonce = await labeledExpand(hpkeSuite, secret, 'base_nonce', context, 12);
  return { key: await crypto.subtle.importKey('raw', key, 'AES-GCM', false, ['encrypt', 'decrypt']), nonce };
}

/** An ephemeral key pair; tests pass the RFC's. */
export interface Ephemeral {
  privateKey: CryptoKey;
  publicKey: Bytes;
}

async function newEphemeral(): Promise<Ephemeral> {
  const pair = (await crypto.subtle.generateKey(ecdh, false, ['deriveBits'])) as CryptoKeyPair;
  return { privateKey: pair.privateKey, publicKey: new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey)) };
}

/** Seals plaintext for the holder of publicKey's private key: the encapsulated key, then the
 * ciphertext. */
export async function seal(publicKey: Bytes, info: Bytes, aad: Bytes, plaintext: Bytes, ephemeral?: Ephemeral): Promise<Bytes> {
  const e = ephemeral ?? (await newEphemeral());
  const { key, nonce } = await schedule(await dh(e.privateKey, publicKey), e.publicKey, publicKey, info);
  const ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: nonce, additionalData: aad }, key, plaintext);
  return concat(e.publicKey, new Uint8Array(ct));
}

/** Opens what seal made, with the private key and its own public key. */
export async function open(privateKey: CryptoKey, publicKey: Bytes, info: Bytes, aad: Bytes, sealed: Bytes): Promise<Bytes> {
  if (sealed.length < 65 + 16) throw new SealError('too short');
  const enc = sealed.subarray(0, 65);
  const { key, nonce } = await schedule(await dh(privateKey, enc), enc, publicKey, info);
  try {
    return new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: nonce, additionalData: aad }, key, sealed.subarray(65)));
  } catch {
    throw new SealError("can't be opened");
  }
}
