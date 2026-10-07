// What a PIN guest checks before sending (docs/e2ee-plan.md): with a link that names the root, the
// folder's key must be signed by it, following the chain of roots; without one, the server's word.
import { describe, expect, it, vi } from 'vitest';
import type * as Api from '../src/api';
import { b64u } from '../src/e2ee/bytes';
import { fingerprint, folderKeyMessage, generateKeyPair, lock, purposes, rootContext, rootMessage, secretKey, sign, type KeyPair } from '../src/e2ee/formats';
import { follow, guestKey, pinLinkRoot, SendRefused } from '../src/e2ee/trust';

const pinKeys = vi.hoisted(() => ({ root: null as string | null }));
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal<typeof Api>()),
  getPinKeys: async () => ({ folder: 'f1', keys: [], root: pinKeys.root }),
}));

const signed = async (by: KeyPair, m: Uint8Array) => b64u(await sign(by.raw!, by.publicKey, m as Uint8Array<ArrayBuffer>));

describe('a guest', () => {
  it('over plain http, sends into a plain folder, and into an encrypted one nothing', async () => {
    Object.defineProperty(globalThis, 'isSecureContext', { value: false, configurable: true });
    try {
      const key = { folder: 'f1', version: 1, public_key: 'AAAA', signature: 'AAAA', encrypted: true };
      const session = { kind: 'pin', pin_kind: 'day', expires_at: null, folder_name: null, shows_folder: false, folder_key: key, roots: [] } as unknown as Api.Session;
      await expect(guestKey(session, null)).rejects.toBeInstanceOf(SendRefused);
      expect(await guestKey({ ...session, folder_key: { ...key, encrypted: false } }, { fingerprint: 'AAAA' })).toBeNull();
      expect(await guestKey({ ...session, folder_key: null }, null)).toBeNull();
    } finally {
      delete (globalThis as { isSecureContext?: boolean }).isSecureContext;
    }
  });

  it("encrypts only for a key the link's root signed, following the chain", async () => {
    const [r1, r2, folder, other] = await Promise.all([generateKeyPair(true), generateKeyPair(true), generateKeyPair(true), generateKeyPair(true)]);
    const roots: Api.RootInfo[] = [
      { public_key: b64u(r1.publicKey), signature: null },
      { public_key: b64u(r2.publicKey), signature: await signed(r1, rootMessage(r2.publicKey)) },
    ];
    expect(b64u(await follow(roots, r1.publicKey))).toBe(roots[1].public_key);
    const key = { folder: 'f1', version: 2, public_key: b64u(folder.publicKey), signature: await signed(r2, folderKeyMessage('f1', 2, folder.publicKey)), encrypted: false };
    const session = { kind: 'pin', pin_kind: 'day', expires_at: null, folder_name: null, shows_folder: false, folder_key: key, roots } as Api.Session;
    const fp = b64u(await fingerprint(r1.publicKey));

    // A link with the first root's fingerprint, or the root itself, finds the newest root's
    // signature good, and encrypts even while the folder is switched off.
    expect((await guestKey(session, { fingerprint: fp }))!.version).toBe(2);
    expect((await guestKey(session, { root: r2.publicKey }))!.version).toBe(2);
    // Typed: the server's word.
    expect(await guestKey(session, null)).toBeNull();
    expect((await guestKey({ ...session, folder_key: { ...key, encrypted: true } }, null))!.version).toBe(2);

    // A key the server names of its own, a chain it breaks, another root's link, no key at all.
    const swapped = { ...session, folder_key: { ...key, public_key: b64u(other.publicKey) } };
    await expect(guestKey(swapped, { fingerprint: fp })).rejects.toBeInstanceOf(SendRefused);
    const broken = { ...session, roots: [roots[0], { ...roots[1], signature: await signed(other, rootMessage(r2.publicKey)) }] };
    await expect(guestKey(broken, { fingerprint: fp })).rejects.toBeInstanceOf(SendRefused);
    await expect(guestKey(session, { fingerprint: b64u(await fingerprint(other.publicKey)) })).rejects.toBeInstanceOf(SendRefused);
    await expect(guestKey({ ...session, folder_key: null }, { fingerprint: fp })).rejects.toBeInstanceOf(SendRefused);
    await expect(guestKey(session, { root: null })).rejects.toBeInstanceOf(SendRefused);
  });

  it("opens the root with the secret of a link of a PIN that shows its folder", async () => {
    const r = await generateKeyPair(true);
    const secret = new Uint8Array(32).fill(3);
    pinKeys.root = b64u(await lock(await secretKey(secret, purposes.pin), rootContext, r.publicKey));
    expect(await pinLinkRoot(b64u(secret))).toEqual(r.publicKey);
    expect(await pinLinkRoot(b64u(new Uint8Array(32).fill(4)))).toBeNull();
    pinKeys.root = null;
    expect(await pinLinkRoot(b64u(secret))).toBeNull();
  });
});
