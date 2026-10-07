// What a phone or browser checks before it sends (docs/e2ee-plan.md): a folder's key counts only
// signed by the root it trusts, the newest the chain of roots leads to from the one it learned.
// Small, for the PIN page as well as the keyring.
import { getPinKeys, type RootInfo, type Session } from '../api';
import { b64u, fromB64u, type Bytes } from './bytes';
import { fingerprint, folderKeyMessage, purposes, rootContext, rootMessage, secretKey, unlock, verify } from './formats';

/** The key to encrypt new files into a folder for: its newest version's public key. */
export interface FolderPublicKey {
  folder: string;
  version: number;
  publicKey: Bytes;
}

/** Nothing goes into a folder: what the server says about its keys can't be checked. */
export class SendRefused extends Error {}

/** Whether this page can encrypt: browsers offer their cryptography only to pages opened over
 * https or on localhost, not over plain http. */
export function canEncrypt(): boolean {
  return !!globalThis.crypto?.subtle && globalThis.isSecureContext !== false;
}

/** The newest root the chain leads to from root, each signed by the one before; root itself
 * where the chain doesn't name it. */
export async function follow(roots: RootInfo[], root: Bytes): Promise<Bytes> {
  const at = roots.findIndex((r) => r.public_key === b64u(root));
  let newest = root;
  for (let i = at + 1; at >= 0 && i < roots.length; i++) {
    const next = fromB64u(roots[i].public_key);
    const sig = roots[i].signature;
    if (!sig || !(await verify(newest, rootMessage(next), fromB64u(sig)))) break;
    newest = next;
  }
  return newest;
}

/** What a PIN's link says of the root: its fingerprint, for a PIN that only sends into a folder
 * with keys, or the root itself, which the secret of a PIN that shows its folder opens; null for
 * a typed code, or a link without either. */
export interface Anchor {
  fingerprint?: string;
  root?: Bytes | null;
}

/** The root the secret of a PIN link that shows its folder opens; null when it doesn't. */
export async function pinLinkRoot(secret: string): Promise<Bytes | null> {
  try {
    const k = await getPinKeys();
    return k.root ? await unlock(await secretKey(fromB64u(secret), purposes.pin), rootContext, fromB64u(k.root)) : null;
  } catch {
    return null;
  }
}

/** A PIN guest's key to encrypt for. A link that names the root takes the folder's newest key
 * only signed by the newest root the chain leads to from it, and always encrypts: SendRefused
 * otherwise. Without one, the server's word counts: encrypted while the folder says so, plain
 * otherwise. */
export async function guestKey(s: Session, anchor: Anchor | null): Promise<FolderPublicKey | null> {
  const k = s.folder_key;
  // Over plain http nothing encrypts, and a page that came unprotected itself gains nothing from
  // checking a signature: a plain folder takes files, an encrypted one none.
  if (!canEncrypt()) {
    if (k?.encrypted) throw new SendRefused("this page can't encrypt");
    return null;
  }
  if (!anchor) return k?.encrypted ? { folder: k.folder, version: k.version, publicKey: fromB64u(k.public_key) } : null;
  let root = anchor.root ?? null;
  for (const r of s.roots) {
    if (root || !anchor.fingerprint) break;
    if (b64u(await fingerprint(fromB64u(r.public_key))) === anchor.fingerprint) root = fromB64u(r.public_key);
  }
  if (!root || !k) throw new SendRefused("the link's root isn't there, or the folder has no key");
  root = await follow(s.roots, root);
  const publicKey = fromB64u(k.public_key);
  if (!(await verify(root, folderKeyMessage(k.folder, k.version, publicKey), fromB64u(k.signature)))) throw new SendRefused("the folder's key isn't signed by the link's root");
  return { folder: k.folder, version: k.version, publicKey };
}
