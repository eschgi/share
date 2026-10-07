// The keyring's flows against a small fake of the server's keys API (docs/e2ee-plan.md): who
// holds what, what is on whose to-do list, and what each browser can open after its turn.
import { describe, expect, it, vi } from 'vitest';
import type * as Api from '../src/api';
import { fromB64u } from '../src/e2ee/bytes';
import { SealError } from '../src/e2ee/formats';
import { Keyring, keysFromInvite, NeedsRoot, SendRefused } from '../src/e2ee/keyring';
import { newSeal } from '../src/e2ee/upload';

interface FakePerson {
  admin: boolean;
  publicKey: string | null;
  /** The person's key, sealed for each of their devices. */
  sealed: Map<string, string>;
  lock: string | null;
  note: string | null;
  folders: Set<string>;
}

const fake = vi.hoisted(() => ({
  caller: null as { user: string; device: string } | { pin: string } | null,
  devices: new Map<string, { user: string; publicKey: string | null }>(),
  people: new Map<string, FakePerson>(),
  /** Each folder's key versions: versions[v - 1] is version v's public key, with the root's
   * signature; and the root's plain statement, while it sends plain. */
  folders: new Map<string, { name: string; encrypted: boolean; versions: { public_key: string; signature: string }[]; plain: string | null }>(),
  /** Folder keys sealed for people, by folder:version:user. */
  sealed: new Map<string, string>(),
  /** The chain of roots; the newest is the recovery key. */
  roots: [] as { public_key: string; signature: string | null; locked: string }[],
  /** The newest root's private key, sealed for admins. */
  rootGrants: new Map<string, string>(),
  recoverySealed: new Map<string, string>(),
  pins: new Map<string, { folder: string; secret_locked: string; secret_version: number; root: string | null; locked: Map<number, string> }>(),
  rekey: new Set<string>(),
  /** No answer at all. */
  offline: false,
  /** The open checks, by id. */
  checks: new Map<string, { asker: string; device: string | null; user: string | null; commitment: string; answer: string | null; answeredBy: string | null; reveal: string | null; confirmation: string | null }>(),
  nextCheck: 0,
  /** A server that names another key for a device that waits, as the asking browser sees it. */
  swapped: new Map<string, string>(),
  /** Devices not seen lately, which can't answer a check. */
  away: new Set<string>(),
}));

vi.mock('../src/e2ee/store', () => {
  const kept = new Map<string, unknown>();
  const update = (id: string, change: object) => {
    const k = kept.get(id) as object | undefined;
    if (k) kept.set(id, { ...k, ...change });
  };
  return {
    loadDeviceKey: async (id: string) => kept.get(id) ?? null,
    saveDeviceKey: async (k: { deviceId: string }) => void kept.set(k.deviceId, k),
    keepOnly: async () => {},
    saveTrusted: async (id: string, trusted: Record<string, string>) => update(id, { trusted }),
    savePins: async (id: string, pins: object) => update(id, { pins }),
  };
});

vi.mock('../src/api', async (importOriginal) => {
  const real = await importOriginal<typeof Api>();
  // The server refuses fields a grant doesn't have, as the contract shows them.
  const grants = (await import('../../contract/api/keys_grants.json')).default.request as Record<string, object[]>;
  const strict = (g: Api.Grants) => {
    for (const [list, entries] of Object.entries(g) as [string, object[]][]) {
      const allowed = Object.keys(grants[list][0]);
      for (const e of entries) for (const k of Object.keys(e)) if (!allowed.includes(k)) throw new real.ApiError(400, 'bad_request', `Invalid request body: unknown field "${k}"`);
    }
  };
  const conflict = (code: string) => new real.ApiError(409, code, code);
  const member = () => {
    const c = fake.caller;
    if (!c || !('user' in c)) throw new real.ApiError(401, 'unauthorized', 'no account');
    return { c, p: fake.people.get(c.user)! };
  };
  const sees = (p: FakePerson, folder: string) => p.admin || p.folders.has(folder);
  const versions = () => [...fake.folders].flatMap(([folder, f]) => f.versions.map((v, i) => ({ folder, version: i + 1, ...v })));
  const info = (id: string) => {
    const f = fake.folders.get(id)!;
    return { id, name: f.name, encrypted: f.encrypted, key_version: f.versions.length || null, plain_signature: f.plain } as unknown as Api.FolderInfo;
  };
  const confirmed = (device: string) => [...fake.checks.values()].some((x) => x.device === device && x.confirmation);
  return {
    ...real,
    getKeys: async (): Promise<Api.KeysAnswer> => {
      if (fake.offline) throw new real.ApiError(0, 'network', 'no answer');
      const { c, p } = member();
      const held = (folder: string, version: number, user = c.user) => fake.sealed.has(`${folder}:${version}:${user}`);
      const mine = versions().filter((v) => sees(p, v.folder));
      const holdsRoot = fake.rootGrants.has(c.user);
      return {
        device_key: fake.devices.get(c.device)!.publicKey,
        person: { public_key: p.publicKey, sealed: p.sealed.get(c.device) ?? null, password_lock: p.lock, held_by: [...p.sealed.keys()].filter((d) => fake.devices.has(d)).length, note: p.note },
        folders: mine.map((v) => ({ ...v, sealed: fake.sealed.get(`${v.folder}:${v.version}:${c.user}`) ?? null })),
        roots: fake.roots.map(({ public_key, signature }) => ({ public_key, signature })),
        root_sealed: fake.rootGrants.get(c.user) ?? null,
        todo: {
          devices: p.publicKey
            ? [...fake.devices]
                .filter(([id, d]) => d.user === c.user && d.publicKey && !p.sealed.has(id) && !confirmed(id))
                .map(([id, d]) => ({ id, public_key: fake.swapped.get(id) ?? d.publicKey!, name: id, client: 'web' as const, created_at: '2026-10-06T10:00:00Z', active: !fake.away.has(id) }))
            : [],
          // Only admins pass folder keys on to other people.
          people: mine
            .filter((v) => p.admin && held(v.folder, v.version))
            .flatMap((v) =>
              [...fake.people]
                .filter(([u, q]) => u !== c.user && q.publicKey && sees(q, v.folder) && !held(v.folder, v.version, u))
                .map(([u, q]) => ({
                  folder: v.folder,
                  version: v.version,
                  user: u,
                  name: u,
                  public_key: q.publicKey!,
                  active: [...q.sealed.keys()].some((d) => fake.devices.has(d) && !fake.away.has(d)),
                })),
            ),
          roots:
            p.admin && holdsRoot
              ? [...fake.people]
                  .filter(([u, q]) => u !== c.user && q.admin && q.publicKey && !fake.rootGrants.has(u))
                  .map(([u, q]) => ({ user: u, name: u, public_key: q.publicKey!, active: [...q.sealed.keys()].some((d) => fake.devices.has(d) && !fake.away.has(d)) }))
              : [],
          recovery: fake.roots.length ? mine.filter((v) => held(v.folder, v.version) && !fake.recoverySealed.has(`${v.folder}:${v.version}`)).map(({ folder, version }) => ({ folder, version })) : [],
          rekey: holdsRoot ? [...fake.rekey] : [],
          pins: [...fake.pins].flatMap(([pin, x]) =>
            fake.folders
              .get(x.folder)!
              .versions.map((_, i) => i + 1)
              .filter((v) => !x.locked.has(v) && held(x.folder, v) && held(x.folder, x.secret_version))
              .map((version) => ({ pin, folder: x.folder, version, secret_version: x.secret_version, secret_locked: x.secret_locked })),
          ),
        },
        checks: [...fake.checks]
          .filter(([, x]) => x.asker === c.device || x.device === c.device || (x.user === c.user && (!x.answeredBy || x.answeredBy === c.device) && p.sealed.has(c.device)))
          .map(([id, x]) => ({
            id,
            asking: x.asker === c.device,
            device: x.device,
            user: x.user,
            from: x.asker,
            commitment: x.commitment,
            answer: x.answer,
            answered: x.answeredBy === c.device,
            reveal: x.reveal,
            confirmation: x.confirmation,
          })),
      };
    },
    openCheck: async (target: { device: string } | { user: string }, commitment: string) => {
      const { c, p } = member();
      const device = 'device' in target ? target.device : null;
      const user = 'user' in target ? target.user : null;
      if (user && !p.admin) throw new real.ApiError(403, 'forbidden', 'only admins pass folder keys on to other people');
      const d = device ? fake.devices.get(device) : null;
      const ok = p.sealed.has(c.device) && (device ? !!d && d.user === c.user && !!d.publicKey && !p.sealed.has(device) : user !== c.user && !!fake.people.get(user!)?.publicKey);
      if (!ok) throw new real.ApiError(404, 'not_found', 'nobody waits there');
      for (const [id, x] of fake.checks) if (x.asker === c.device && x.device === device && x.user === user) fake.checks.delete(id);
      const id = `check${++fake.nextCheck}`;
      fake.checks.set(id, { asker: c.device, device, user, commitment, answer: null, answeredBy: null, reveal: null, confirmation: null });
      return { id };
    },
    answerCheck: async (id: string, key: string) => {
      const { c, p } = member();
      const x = fake.checks.get(id);
      if (!x || x.reveal || !(x.device === c.device || (x.user === c.user && (!x.answeredBy || x.answeredBy === c.device) && p.sealed.has(c.device)))) {
        throw new real.ApiError(404, 'not_found', 'no such check');
      }
      x.answer = key;
      x.answeredBy = c.device;
    },
    revealCheck: async (id: string, key: string, answer: string) => {
      const { c } = member();
      const x = fake.checks.get(id);
      if (!x || x.asker !== c.device) throw new real.ApiError(404, 'not_found', 'no such check');
      if (x.answer !== answer || x.reveal) throw new real.ApiError(409, 'not_answered', 'nothing to reveal for');
      const { b64u, concat, fromB64u, utf8 } = await import('../src/e2ee/bytes');
      const sum = new Uint8Array(await crypto.subtle.digest('SHA-256', concat(utf8('share-e2ee-v1/check'), fromB64u(key))));
      if (b64u(sum) !== x.commitment) throw new real.ApiError(400, 'bad_request', 'not the key committed to');
      x.reveal = key;
    },
    confirmCheck: async (id: string, confirmation: string) => {
      const { c } = member();
      const x = fake.checks.get(id);
      if (!x || x.asker !== c.device || !x.reveal || x.confirmation) throw new real.ApiError(409, 'not_answered', 'not revealed');
      x.confirmation = confirmation;
    },
    closeCheck: async (id: string) => {
      member();
      fake.checks.delete(id);
    },
    signOutDevice: async (id: string) => {
      const { c, p } = member();
      if (fake.devices.get(id)?.user !== c.user) throw new real.ApiError(404, 'not_found', 'not theirs');
      fake.devices.delete(id);
      p.sealed.delete(id);
      for (const [cid, x] of fake.checks) if (x.device === id || x.asker === id) fake.checks.delete(cid);
    },
    putDeviceKey: async (publicKey: string) => {
      const { c, p } = member();
      fake.devices.get(c.device)!.publicKey = publicKey;
      p.sealed.delete(c.device);
    },
    putPersonKey: async (publicKey: string, sealed: string, startOver = false) => {
      const { c, p } = member();
      if (p.publicKey && !startOver) throw conflict('key_exists');
      if (startOver) {
        p.sealed.clear();
        p.lock = null;
        p.note = null;
        fake.rootGrants.delete(c.user);
        for (const k of [...fake.sealed.keys()]) if (k.endsWith(`:${c.user}`)) fake.sealed.delete(k);
      }
      p.publicKey = publicKey;
      p.sealed.set(c.device, sealed);
    },
    putPasswordLock: async (lock: string) => {
      const { p } = member();
      if (!p.publicKey) throw conflict('no_key');
      p.lock = lock;
    },
    putNote: async (note: string) => {
      const { p } = member();
      if (!p.publicKey) throw conflict('no_key');
      p.note = note;
    },
    postGrants: async (g: Api.Grants) => {
      strict(g);
      const { c, p } = member();
      for (const d of g.devices) {
        if (fake.devices.get(d.device)?.user !== c.user) throw new real.ApiError(400, 'bad_request', 'not their device');
        p.sealed.set(d.device, d.sealed);
      }
      // A member's keys for someone else are left out.
      for (const x of g.people) if (x.user === c.user || p.admin) fake.sealed.set(`${x.folder}:${x.version}:${x.user}`, x.sealed);
      for (const x of g.recovery) fake.recoverySealed.set(`${x.folder}:${x.version}`, x.sealed);
      for (const x of g.pins) fake.pins.get(x.pin)!.locked.set(x.version, x.locked);
      // The root's private key only for an admin, from themselves or an admin who holds it.
      for (const x of g.roots) if (fake.people.get(x.user)?.admin && (x.user === c.user || (p.admin && fake.rootGrants.has(c.user)))) fake.rootGrants.set(x.user, x.sealed);
    },
    createFolder: async (name: string, signed?: { id: string; key?: Api.NewFolderKey; plain_signature?: string }) => {
      member();
      if (fake.roots.length && !signed) throw new real.ApiError(400, 'bad_signature', 'unsigned');
      const id = signed?.id ?? name;
      fake.folders.set(id, { name, encrypted: false, versions: [], plain: signed?.plain_signature ?? null });
      return info(id);
    },
    renameFolder: async (id: string, name: string, plain_signature?: string) => {
      member();
      const f = fake.folders.get(id)!;
      if (!f.encrypted && fake.roots.length && !plain_signature) throw new real.ApiError(400, 'bad_signature', 'unsigned');
      f.name = name;
      if (!f.encrypted) f.plain = plain_signature ?? null;
      return info(id);
    },
    setFolderEncryption: async (folder: string, change: { encrypted: true; key: Api.NewFolderKey } | { encrypted: false; plain_signature: string }) => {
      const { c } = member();
      const f = fake.folders.get(folder)!;
      if (change.encrypted) {
        if (!fake.roots.length) throw conflict('no_key');
        const version = f.versions.length + 1;
        f.versions.push({ public_key: change.key.public_key, signature: change.key.signature });
        fake.sealed.set(`${folder}:${version}:${c.user}`, change.key.sealed);
        fake.recoverySealed.set(`${folder}:${version}`, change.key.recovery_sealed);
        f.plain = null;
      } else {
        f.plain = change.plain_signature;
      }
      f.encrypted = change.encrypted;
      return info(folder);
    },
    postFolderKey: async (folder: string, version: number, key: Api.NewFolderKey) => {
      const { c } = member();
      const f = fake.folders.get(folder)!;
      if (version !== f.versions.length + 1) throw conflict('key_outdated');
      f.versions.push({ public_key: key.public_key, signature: key.signature });
      fake.sealed.set(`${folder}:${version}:${c.user}`, key.sealed);
      fake.recoverySealed.set(`${folder}:${version}`, key.recovery_sealed);
      fake.rekey.delete(folder);
    },
    putRecovery: async (r: Api.NewRecovery) => {
      const { c } = member();
      if (!fake.roots.length !== !r.signature) throw new real.ApiError(400, 'bad_signature', 'the chain');
      fake.roots.push({ public_key: r.public_key, signature: r.signature, locked: r.locked });
      fake.rootGrants.clear();
      fake.rootGrants.set(c.user, r.sealed);
      for (const k of r.folder_keys) fake.folders.get(k.folder)!.versions[k.version - 1].signature = k.signature;
      for (const k of r.plain) fake.folders.get(k.folder)!.plain = k.signature;
      fake.recoverySealed.clear(); // sealed again for the new key, from the to-do lists
    },
    getRecovery: async (): Promise<Api.Recovery> => {
      member();
      const newest = fake.roots.at(-1);
      return {
        public_key: newest?.public_key ?? null,
        locked: newest?.locked ?? null,
        roots: fake.roots.map(({ public_key, signature }) => ({ public_key, signature })),
        folders: [...fake.recoverySealed].map(([k, sealed]) => {
          const [folder, v] = k.split(':');
          return { folder, version: Number(v), ...fake.folders.get(folder)!.versions[Number(v) - 1], sealed };
        }),
        sign: {
          folder_keys: versions().map(({ folder, version, public_key, signature }) => ({ folder, version, public_key, signature })),
          plain: [...fake.folders]
            .filter(([, f]) => !fake.roots.length || (f.plain && !f.encrypted))
            .map(([folder, f]) => ({ folder, version: f.versions.length, name: f.name, signature: f.plain })),
        },
      };
    },
    getPinKeys: async (): Promise<Api.PinKeys> => {
      const c = fake.caller;
      if (!c || !('pin' in c)) throw new real.ApiError(401, 'unauthorized', 'no PIN');
      const x = fake.pins.get(c.pin)!;
      return { folder: x.folder, root: x.root, keys: [...x.locked].map(([version, locked]) => ({ version, public_key: fake.folders.get(x.folder)!.versions[version - 1].public_key, locked })) };
    },
  };
});

function person(id: string, admin: boolean, folders: string[] = []) {
  fake.people.set(id, { admin, publicKey: null, sealed: new Map(), lock: null, note: null, folders: new Set(folders) });
}

/** A new phone or browser of a person, with its own keyring. */
function browser(user: string, device: string): { ring: Keyring; me: Api.Me } {
  fake.devices.set(device, { user, publicKey: null });
  const me = { user: { id: user, name: user, role: fake.people.get(user)!.admin ? 'admin' : 'member' }, device: { id: device, name: device } } as unknown as Api.Me;
  return { ring: new Keyring(), me };
}

/** A browser that was there before, on a page opened anew: what it keeps comes back. */
function reopen(user: string, device: string): { ring: Keyring; me: Api.Me } {
  const me = { user: { id: user, name: user, role: fake.people.get(user)!.admin ? 'admin' : 'member' }, device: { id: device, name: device } } as unknown as Api.Me;
  return { ring: new Keyring(), me };
}

/** Requests from now on come from b. */
function as(b: { me: Api.Me }) {
  fake.caller = { user: b.me.user.id, device: b.me.device.id };
}

type Browser = { ring: Keyring; me: Api.Me };

/** A check to its end: the asking browser lists the ask, the person opens it with Show, which
 * opens the check, the other side answers, the asking browser reveals its nonce, both show the
 * same code, and the person at the asking browser allows it. */
async function approve(asker: Browser, other: Browser, kind: 'device' | 'person', id: string) {
  as(asker);
  await asker.ring.checkIn();
  await asker.ring.show(asker.ring.asks.find((x) => x.kind === kind && x.id === id)!);
  expect(asker.ring.opened).toEqual(expect.objectContaining({ kind, id }));
  for (const b of [other, asker, other]) {
    as(b);
    await b.ring.checkIn();
  }
  const ask = asker.ring.asks.find((x) => x.kind === kind && x.id === id)!;
  expect(ask.code).toMatch(/^\d{6}$/);
  expect(other.ring.codes.map((c) => c.code)).toContain(ask.code);
  as(asker);
  await asker.ring.allow(ask);
  as(other);
  await other.ring.checkIn();
}

/** A folder as GET /api/folders describes it. */
function folderInfo(id: string): Api.FolderInfo {
  const f = fake.folders.get(id)!;
  return { id, name: f.name, encrypted: f.encrypted, key_version: f.versions.length || null, plain_signature: f.plain } as unknown as Api.FolderInfo;
}

describe('keyring', { timeout: 60_000 }, () => {
  for (const id of ['f1', 'f2', 'plain']) fake.folders.set(id, { name: id, encrypted: false, versions: [], plain: null });
  person('ada', true);
  person('max', false, ['f1']);
  person('eve', false, ['f1']);
  person('zed', true);
  const a1 = browser('ada', 'a1');
  const a2 = browser('ada', 'a2');
  let code = '';
  const file = { id: 'file1', folder: 'f1', enc: null as Api.FileEnc | null };
  let fileKey: Uint8Array;

  it("makes the person's key on their first browser", async () => {
    as(a1);
    await a1.ring.start(a1.me);
    expect(a1.ring.status).toBe('ready');
    expect(fake.people.get('ada')!.publicKey).not.toBeNull();
    expect(fake.people.get('ada')!.sealed.has('a1')).toBe(true);
    expect(a1.ring.hasRecovery()).toBe(false);

    // Nobody is asked about while no folder is encrypted.
    const a0 = browser('ada', 'a0');
    as(a0);
    await a0.ring.start(a0.me);
    as(a1);
    await a1.ring.checkIn();
    expect(a1.ring.asks).toEqual([]);
    fake.devices.delete('a0');
  });

  it('makes the recovery key, which signs every folder as plain, then the first folder key, which seals files', async () => {
    as(a1);
    // Before any root, every folder sends plain.
    expect(await a1.ring.sendKey(folderInfo('plain'))).toBeNull();
    await expect(a1.ring.encryptFolder(folderInfo('f1'))).rejects.toBeInstanceOf(NeedsRoot);
    code = await a1.ring.makeRecovery();
    expect(code).toMatch(/^([0-9A-Z]{4}-){7}[0-9A-Z]{4}$/);
    expect(a1.ring.hasRecovery()).toBe(true);
    expect(fake.rootGrants.has('ada')).toBe(true);
    expect([...fake.folders.values()].every((f) => f.plain)).toBe(true);
    await a1.ring.encryptFolder(folderInfo('f1'));
    expect(fake.folders.get('f1')!.versions).toHaveLength(1);
    expect(fake.folders.get('f1')!.plain).toBeNull();
    expect(fake.recoverySealed.has('f1:1')).toBe(true);
    expect(a1.ring.hasFolderKey('f1', 1)).toBe(true);
    const target = (await a1.ring.sendKey(folderInfo('f1')))!;
    expect(target.version).toBe(1);
    expect(await a1.ring.sendKey(folderInfo('plain'))).toBeNull();

    const seal = await newSeal(target, 1000, '1');
    file.enc = { version: 1, key: seal.sealed, header: seal.header };
    fileKey = fromB64u(seal.key);
    expect(await a1.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc })).toEqual(fileKey);
  });

  it("lets another browser wait until one with the key passes it on, after a check", async () => {
    as(a2);
    await a2.ring.start(a2.me);
    expect(a2.ring.status).toBe('waiting');
    expect(a2.ring.hasFolderKey('f1', 1)).toBe(false);
    await expect(a2.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).rejects.toBeInstanceOf(SealError);

    // The browser with the key lists it, and opens nothing by itself: no check before Show, and
    // nothing sealed until the person compared the codes.
    as(a1);
    await a1.ring.checkIn();
    expect(a1.ring.asks).toEqual([expect.objectContaining({ kind: 'device', id: 'a2', name: 'a2', client: 'web', code: null })]);
    expect(a1.ring.opened).toBeNull();
    expect(fake.checks.size).toBe(0);
    expect(a1.ring.pace()).toBe(30_000);
    await expect(a1.ring.allow(a1.ring.asks[0])).rejects.toBeInstanceOf(SealError);
    as(a2);
    await a2.ring.checkIn();
    expect(a2.ring.codes).toEqual([]);
    expect(fake.people.get('ada')!.sealed.has('a2')).toBe(false);

    await approve(a1, a2, 'device', 'a2');
    expect(a2.ring.status).toBe('ready');
    expect(await a2.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);
    expect(a1.ring.asks).toEqual([]);
  });

  it("signs out a device the person here doesn't know, and shows two codes when the server swaps a key", async () => {
    const x1 = browser('ada', 'x1');
    as(x1);
    await x1.ring.start(x1.me);
    as(a1);
    await a1.ring.checkIn();
    await a1.ring.deny(a1.ring.asks.find((k) => k.id === 'x1')!);
    expect(fake.devices.has('x1')).toBe(false);
    await a1.ring.checkIn();
    expect(a1.ring.asks.some((k) => k.id === 'x1')).toBe(false);

    // A server that names its own key for a device that waits can't make the codes match.
    const y1 = browser('ada', 'y1');
    as(y1);
    await y1.ring.start(y1.me);
    fake.swapped.set('y1', fake.people.get('max')!.publicKey ?? fake.people.get('ada')!.publicKey!);
    as(a1);
    await a1.ring.checkIn();
    await a1.ring.show(a1.ring.asks.find((k) => k.id === 'y1')!);
    for (const b of [y1, a1, y1]) {
      as(b);
      await b.ring.checkIn();
    }
    const ask = a1.ring.asks.find((k) => k.id === 'y1')!;
    expect(ask.code).toMatch(/^\d{6}$/);
    expect(y1.ring.codes).toHaveLength(1);
    expect(y1.ring.codes[0].code).not.toBe(ask.code);
    fake.swapped.delete('y1');
    as(a1);
    await a1.ring.deny(ask);
  });

  it('checks in quietly: seals for a browser that waits, which then opens it, never showing loading', async () => {
    const aw = browser('ada', 'aw');
    as(aw);
    await aw.ring.start(aw.me);
    expect(aw.ring.status).toBe('waiting');
    const seen: string[] = [];
    aw.ring.watch(() => seen.push(aw.ring.status));
    await approve(a1, aw, 'device', 'aw');
    expect(aw.ring.status).toBe('ready');
    expect(seen).not.toContain('loading');
    expect(await aw.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);

    // Without an answer, a check-in keeps what is open, and the status.
    fake.offline = true;
    try {
      await aw.ring.checkIn();
    } finally {
      fake.offline = false;
    }
    expect(aw.ring.status).toBe('ready');
    expect(seen).not.toContain('failed');
    expect(await aw.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);
  });

  it('lists only phones and browsers seen lately, and checks in sooner while a check runs', async () => {
    const az = browser('ada', 'az');
    as(az);
    await az.ring.start(az.me);
    expect(az.ring.pace()).toBe(5_000);
    fake.away.add('az');
    as(a1);
    await a1.ring.checkIn();
    expect(a1.ring.asks).toEqual([]);
    expect(a1.ring.pace()).toBe(30_000);

    fake.away.delete('az');
    await a1.ring.checkIn();
    expect(a1.ring.asks.map((k) => k.id)).toEqual(['az']);
    expect(a1.ring.pace()).toBe(30_000);
    await a1.ring.show(a1.ring.asks[0]);
    expect(a1.ring.pace()).toBe(2_000);
    as(az);
    await az.ring.checkIn();
    expect(az.ring.pace()).toBe(2_000);

    // Not now: the check ends, on both sides, and the ask stays listed.
    as(a1);
    await a1.ring.hide(a1.ring.asks[0]);
    expect(a1.ring.opened).toBeNull();
    expect(a1.ring.pace()).toBe(30_000);
    expect([...fake.checks.values()].some((x) => x.device === 'az')).toBe(false);
    await a1.ring.checkIn();
    expect(a1.ring.asks.map((k) => k.id)).toEqual(['az']);
    as(az);
    await az.ring.checkIn();
    expect(az.ring.pace()).toBe(5_000);
    as(a1);
    await a1.ring.deny(a1.ring.asks[0]);
    expect(a1.ring.asks).toEqual([]);
  });

  it("can't be made to show the same code by a server that changes what it relays afterwards", async () => {
    const at = browser('ada', 'at');
    as(at);
    await at.ring.start(at.me);
    as(a1);
    await a1.ring.checkIn();
    await a1.ring.show(a1.ring.asks.find((k) => k.id === 'at')!);
    as(at);
    await at.ring.checkIn();
    const [id, x] = [...fake.checks].find(([, c]) => c.device === 'at')!;
    // A commitment and a nonce of the server's own, after the browser answered: no code.
    const { b64u, concat, utf8 } = await import('../src/e2ee/bytes');
    const own = new Uint8Array(32).fill(7);
    const real = x.commitment;
    x.commitment = b64u(new Uint8Array(await crypto.subtle.digest('SHA-256', concat(utf8('share-e2ee-v1/check'), own))));
    x.reveal = b64u(own);
    as(at);
    await at.ring.checkIn();
    expect(at.ring.codes).toEqual([]);

    // The asking browser's code stays the one made from the answer it revealed for.
    x.commitment = real;
    x.reveal = null;
    as(at);
    await at.ring.checkIn(); // the page has its nonce: it doesn't answer again
    as(a1);
    await a1.ring.checkIn();
    const code = a1.ring.asks.find((k) => k.id === 'at')!.code;
    expect(code).toMatch(/^\d{6}$/);
    expect(fake.checks.get(id)!.reveal).not.toBeNull();
    fake.checks.get(id)!.answer = b64u(new Uint8Array(32).fill(9));
    await a1.ring.checkIn();
    expect(a1.ring.asks.find((k) => k.id === 'at')!.code).toBe(code);
    as(at);
    await at.ring.checkIn();
    expect(at.ring.codes).toEqual([{ kind: 'device', from: 'a1', code }]);
    as(a1);
    await a1.ring.deny(a1.ring.asks.find((k) => k.id === 'at')!);
  });

  it('opens the keys in a new browser with the password, and not with a wrong one', async () => {
    as(a1);
    await a1.ring.signedIn(a1.me, 'correct horse battery');
    expect(fake.people.get('ada')!.lock).not.toBeNull();

    const wrong = browser('ada', 'a3');
    as(wrong);
    await wrong.ring.signedIn(wrong.me, 'wrong horse battery');
    expect(wrong.ring.status).toBe('waiting');

    const a4 = browser('ada', 'a4');
    as(a4);
    await a4.ring.signedIn(a4.me, 'correct horse battery');
    expect(a4.ring.status).toBe('ready');
    expect(a4.ring.hasFolderKey('f1', 1)).toBe(true);
    expect(fake.people.get('ada')!.sealed.has('a4')).toBe(true);
    // It doesn't ask about itself, although the list it got before the password opened the key named it.
    expect(a4.ring.asks.map((k) => k.id)).toEqual(['a3']);

    // The browser with the wrong password waits for a check like any new one.
    await approve(a4, wrong, 'device', 'a3');
    expect(wrong.ring.status).toBe('ready');
  });

  it('seals a folder key for someone who sees the folder, from an admin, after a check', async () => {
    const m1 = browser('max', 'm1');
    as(m1);
    await m1.ring.start(m1.me);
    expect(m1.ring.status).toBe('ready');
    expect(m1.ring.hasFolderKey('f1', 1)).toBe(false);
    expect(m1.ring.waitsForFolders).toBe(true);
    as(a1);
    await a1.ring.checkIn();
    expect(a1.ring.asks).toEqual([expect.objectContaining({ kind: 'person', id: 'max', name: 'max', folders: ['f1'] })]);
    expect(a1.ring.keyChanged(a1.ring.asks[0])).toBe(false);
    await approve(a1, m1, 'person', 'max');
    expect(await m1.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);
    expect(m1.ring.waitsForFolders).toBe(false);
    // Max doesn't see f2, and gets nothing of it.
    as(a1);
    await a1.ring.encryptFolder(folderInfo('f2'));
    as(m1);
    await m1.ring.refresh();
    expect(m1.ring.hasFolderKey('f2', 1)).toBe(false);
    // His browser checks f1's key with the root the check handed on.
    expect((await m1.ring.sendKey(folderInfo('f1')))!.version).toBe(1);
  });

  it('locks a key with the password typed while it waited, once another browser sealed it', async () => {
    const e1 = browser('eve', 'e1');
    as(e1);
    await e1.ring.start(e1.me); // her first browser makes her key, without a password
    await approve(a1, e1, 'person', 'eve'); // the admin seals f1 for her
    expect(e1.ring.hasFolderKey('f1', 1)).toBe(true);
    expect(fake.people.get('eve')!.lock).toBeNull();

    // A password from an admin opens no lock: another browser waits, until hers seals it the key.
    const e2 = browser('eve', 'e2');
    as(e2);
    await e2.ring.signedIn(e2.me, 'eve password');
    expect(e2.ring.status).toBe('waiting');
    await approve(e1, e2, 'device', 'e2');
    expect(e2.ring.status).toBe('ready');
    expect(fake.people.get('eve')!.lock).not.toBeNull();

    // So the next browser opens it with the password at once.
    const e3 = browser('eve', 'e3');
    as(e3);
    await e3.ring.signedIn(e3.me, 'eve password');
    expect(e3.ring.status).toBe('ready');
    expect(e3.ring.hasFolderKey('f1', 1)).toBe(true);
  });

  it("makes a member a new key by itself once theirs is lost, for which an admin's browser seals the folders", async () => {
    // Every browser of hers is signed out, and an admin gives her a new password: the lock goes.
    const eve = fake.people.get('eve')!;
    for (const d of ['e1', 'e2', 'e3']) {
      fake.devices.delete(d);
      eve.sealed.delete(d);
    }
    eve.lock = null;
    const e4 = browser('eve', 'e4');
    as(e4);
    await e4.ring.signedIn(e4.me, 'new eve password');
    expect(e4.ring.status).toBe('ready');
    expect(e4.ring.hasFolderKey('f1', 1)).toBe(false);
    expect(eve.lock).not.toBeNull();
    // Her key is new, so the admin's browser checks it again.
    as(a1);
    await a1.ring.checkIn();
    expect(a1.ring.keyChanged(a1.ring.asks.find((k) => k.kind === 'person' && k.id === 'eve')!)).toBe(true);
    await approve(a1, e4, 'person', 'eve');
    expect(await e4.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);

    // An admin's browser asks instead, as the recovery code opens every folder again.
    const z1 = browser('zed', 'z1');
    as(z1);
    await z1.ring.start(z1.me);
    fake.devices.delete('z1');
    fake.people.get('zed')!.sealed.delete('z1');
    const z2 = browser('zed', 'z2');
    as(z2);
    await z2.ring.signedIn(z2.me, 'zed password');
    expect(z2.ring.status).toBe('waiting');
  });

  it("moves an encrypted file's key to another encrypted folder, never to a plain one", async () => {
    as(a1);
    const [moved] = await a1.ring.moveKeys([{ ...file, enc: file.enc }], 'f2');
    expect(moved.version).toBe(1);
    expect(await a1.ring.fileKey({ id: file.id, folder: 'f2', enc: { ...file.enc!, key: moved.key } })).toEqual(fileKey);
    await expect(a1.ring.moveKeys([{ ...file, enc: file.enc }], 'plain')).rejects.toBeInstanceOf(SealError);
    expect(await a1.ring.moveKeys([{ id: 'x', folder: 'f1', enc: null }], 'plain')).toEqual([]);
  });

  it("brings the folder keys to a new person through an invite link's secret", async () => {
    as(a1);
    const invite = await a1.ring.inviteKeys(['f1']);
    expect(invite.keys.map((k) => k.folder)).toEqual(['f1']);
    expect(invite.root).toBeTruthy();
    const keys: Api.InviteKey[] = invite.keys.map((k) => ({ ...k, ...fake.folders.get(k.folder)!.versions[k.version - 1] }));

    person('eva', false, ['f1']);
    const e1 = browser('eva', 'e1');
    as(e1);
    await keysFromInvite(e1.me, invite.secret, keys, invite.root!);
    await e1.ring.start(e1.me);
    expect(await e1.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);
    expect((await e1.ring.sendKey(folderInfo('f1')))!.version).toBe(1);

    // A link cut before its secret, or with another one, waits for the family.
    person('leo', false, ['f1']);
    const l1 = browser('leo', 'l1');
    as(l1);
    const other = await a1.ring.inviteKeys(['f2']);
    await keysFromInvite(l1.me, other.secret, keys, invite.root!);
    await l1.ring.start(l1.me);
    expect(l1.ring.status).toBe('ready');
    expect(l1.ring.hasFolderKey('f1', 1)).toBe(false);

    // A member who holds f1 doesn't list Leo: only admins pass folder keys on to other people.
    as(e1);
    await e1.ring.checkIn();
    expect(e1.ring.hasFolderKey('f1', 1)).toBe(true);
    expect(e1.ring.asks).toEqual([]);

    // Leo is listed; Show opens his check, and Not now ends it, while he stays listed.
    as(a1);
    await a1.ring.checkIn();
    const leo = a1.ring.asks.find((k) => k.id === 'leo')!;
    expect([...fake.checks.values()].some((x) => x.user === 'leo')).toBe(false);
    await a1.ring.show(leo);
    expect([...fake.checks.values()].some((x) => x.user === 'leo')).toBe(true);
    await a1.ring.hide(leo);
    await a1.ring.checkIn();
    expect(a1.ring.asks.some((k) => k.id === 'leo')).toBe(true);
    expect([...fake.checks.values()].some((x) => x.user === 'leo')).toBe(false);
    expect(fake.people.has('leo') && fake.devices.has('l1')).toBe(true);
  });

  it("brings the person's own key to their new phone through an invite link's secret", async () => {
    as(a1);
    const own = (await a1.ring.personKeyForInvite())!;
    const a5 = browser('ada', 'a5');
    as(a5);
    await keysFromInvite(a5.me, own.secret, [{ folder: null, version: 0, public_key: fake.people.get('ada')!.publicKey!, signature: null, locked: own.locked }], own.root!);
    await a5.ring.start(a5.me);
    expect(a5.ring.status).toBe('ready');
    expect(a5.ring.hasFolderKey('f2', 1)).toBe(true);
  });

  it("opens a PIN's folder for guests with its link's secret, and shows admins the link again", async () => {
    as(a1);
    const made = (await a1.ring.pinSecret('f1'))!;
    expect(made.body.keys.map((k) => k.version)).toEqual([1]);
    fake.pins.set('p1', {
      folder: 'f1',
      secret_locked: made.body.locked,
      secret_version: made.body.version,
      root: made.body.root,
      locked: new Map(made.body.keys.map((k) => [k.version, k.locked])),
    });
    const listed = { folder: 'f1', secret: { locked: made.body.locked, version: made.body.version } };
    expect(await a1.ring.pinLinkSecret(listed)).toBe(made.secret);
    expect(await a1.ring.pinLinkSecret({ folder: 'f1', secret: null })).toBeNull();
    expect(await a1.ring.pinSecret('plain')).toBeNull();

    fake.caller = { pin: 'p1' };
    const guest = new Keyring();
    expect(await guest.openPinKeys(made.secret)).toBe(1);
    expect(await guest.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);
    expect(guest.guestRoot).not.toBeNull();
    const stranger = new Keyring();
    expect(await stranger.openPinKeys((await a1.ring.pinSecret('f1'))!.secret)).toBe(0);
  });

  it('makes a new folder key version when asked, which the PIN and the recovery key get too', async () => {
    fake.rekey.add('f1');
    as(a1);
    await a1.ring.refresh();
    expect(fake.folders.get('f1')!.versions).toHaveLength(2);
    expect((await a1.ring.sendKey(folderInfo('f1')))!.version).toBe(2);
    expect(fake.recoverySealed.has('f1:2')).toBe(true);
    expect(fake.pins.get('p1')!.locked.has(2)).toBe(true);
    // The others who see the folder got the new version on the second pass.
    expect(fake.sealed.has('f1:2:max')).toBe(true);
  });

  it('restores an admin on a new browser with the recovery code', async () => {
    const a6 = browser('ada', 'a6');
    as(a6);
    await a6.ring.start(a6.me);
    expect(a6.ring.status).toBe('waiting');
    await expect(a6.ring.useRecoveryCode('0000-0000-0000-0000-0000-0000-0000-0000')).rejects.toBeInstanceOf(SealError);
    await expect(a6.ring.useRecoveryCode('not a code')).rejects.toBeInstanceOf(SealError);
    expect(await a6.ring.useRecoveryCode(code.toLowerCase())).toBe(3); // f1 twice, f2
    expect(a6.ring.status).toBe('ready');
    expect(await a6.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);
    // It started over with a new person key, which reaches the person's other browsers after a check.
    expect(fake.people.get('ada')!.sealed.has('a1')).toBe(false);
    await approve(a6, a1, 'device', 'a1');
    expect(a1.ring.status).toBe('ready');
    expect(a1.ring.hasFolderKey('f1', 2)).toBe(true);
  });

  it('makes a new recovery code, for which the folder keys are sealed again', async () => {
    as(a1);
    const next = await a1.ring.makeRecovery();
    expect(next).not.toBe(code);
    expect([...fake.recoverySealed.keys()].sort()).toEqual(['f1:1', 'f1:2', 'f2:1']);
    const a7 = browser('ada', 'a7');
    as(a7);
    await a7.ring.start(a7.me);
    await expect(a7.ring.useRecoveryCode(code)).rejects.toBeInstanceOf(SealError);
    expect(await a7.ring.useRecoveryCode(next)).toBe(3);
  });
  // The recovery code gave Ada a new person key on a7, which holds the newest root too.
  const ada = reopen('ada', 'a7');

  it("refuses to send where the server's word about a folder's keys can't be checked", async () => {
    as(ada);
    await ada.ring.start(ada.me);
    expect(ada.ring.status).toBe('ready');
    const f1 = fake.folders.get('f1')!;
    // A version the root didn't sign: version 1's signature, for a key named version 3.
    f1.versions.push({ ...f1.versions[0] });
    await ada.ring.refresh();
    await expect(ada.ring.sendKey(folderInfo('f1'))).rejects.toBeInstanceOf(SendRefused);
    f1.versions.pop();
    // Fewer versions than this browser saw: back to one a removed person may hold.
    const v2 = f1.versions.pop()!;
    await ada.ring.refresh();
    await expect(ada.ring.sendKey(folderInfo('f1'))).rejects.toBeInstanceOf(SendRefused);
    f1.versions.push(v2);
    // Switched off without the root's plain statement: still encrypted, which the server takes.
    f1.encrypted = false;
    await ada.ring.refresh();
    expect((await ada.ring.sendKey(folderInfo('f1')))!.version).toBe(2);
    f1.encrypted = true;
    // A folder nobody signed as plain, one with another folder's statement, and a plain folder
    // under another name: nothing goes in.
    fake.folders.set('new', { name: 'new', encrypted: false, versions: [], plain: null });
    await ada.ring.refresh();
    await expect(ada.ring.sendKey(folderInfo('new'))).rejects.toBeInstanceOf(SendRefused);
    fake.folders.get('new')!.plain = fake.folders.get('plain')!.plain;
    await expect(ada.ring.sendKey(folderInfo('new'))).rejects.toBeInstanceOf(SendRefused);
    fake.folders.delete('new');
    fake.folders.get('plain')!.name = 'f1';
    await expect(ada.ring.sendKey(folderInfo('plain'))).rejects.toBeInstanceOf(SendRefused);
    fake.folders.get('plain')!.name = 'plain';
    expect(await ada.ring.sendKey(folderInfo('plain'))).toBeNull();
  });

  it("seals nothing for a recovery key the server swaps, and locks no folder key for a secret it can't open", async () => {
    const { b64u, randomBytes } = await import('../src/e2ee/bytes');
    const { folderContext, generateKeyPair, purposes, sealKey } = await import('../src/e2ee/formats');
    const own = await generateKeyPair(true);
    fake.roots.push({ public_key: b64u(own.publicKey), signature: b64u(new Uint8Array(64)), locked: '' });
    fake.recoverySealed.clear();
    as(ada);
    try {
      await ada.ring.refresh();
      expect(fake.recoverySealed.size).toBe(0);
    } finally {
      fake.roots.pop();
    }
    await ada.ring.refresh();
    expect([...fake.recoverySealed.keys()].sort()).toEqual(['f1:1', 'f1:2', 'f2:1']);

    // Anyone can seal a secret for a folder's public key: a PIN with one gets nothing locked.
    const secret = randomBytes(32);
    const sealed = await sealKey(fromB64u(fake.folders.get('f1')!.versions[0].public_key), purposes.pin, folderContext('f1', 1), secret);
    fake.pins.set('p2', { folder: 'f1', secret_locked: b64u(sealed), secret_version: 1, root: null, locked: new Map() });
    await ada.ring.refresh();
    expect(fake.pins.get('p2')!.locked.size).toBe(0);
    expect(fake.pins.get('p1')!.locked.size).toBe(2);
    fake.pins.delete('p2');
  });

  it("trusts the root of the person's note in a new browser, not one the server names", async () => {
    const { b64u } = await import('../src/e2ee/bytes');
    const { folderKeyMessage, generateKeyPair, sign } = await import('../src/e2ee/formats');
    as(ada);
    await ada.ring.signedIn(ada.me, 'correct horse battery');
    expect(fake.people.get('ada')!.note).not.toBeNull();
    // The server names a root of its own, which signs a version of f1's key of its own.
    const own = await generateKeyPair(true);
    const next = await generateKeyPair(true);
    const kept = fake.roots;
    const f1 = fake.folders.get('f1')!;
    fake.roots = [{ public_key: b64u(own.publicKey), signature: null, locked: '' }];
    f1.versions.push({ public_key: b64u(next.publicKey), signature: b64u(await sign(own.raw!, own.publicKey, folderKeyMessage('f1', 3, next.publicKey))) });
    const a8 = browser('ada', 'a8');
    try {
      as(a8);
      await a8.ring.signedIn(a8.me, 'correct horse battery');
      expect(a8.ring.status).toBe('ready');
      await expect(a8.ring.sendKey(folderInfo('f1'))).rejects.toBeInstanceOf(SendRefused);
    } finally {
      fake.roots = kept;
      f1.versions.pop();
    }
    await a8.ring.refresh();
    expect((await a8.ring.sendKey(folderInfo('f1')))!.version).toBe(2);
  });

  it("follows the chain of roots to a new recovery key, also on a browser that waits for its person's key", async () => {
    as(a2);
    await a2.ring.refresh();
    expect(a2.ring.status).toBe('waiting');
    expect((await a2.ring.sendKey(folderInfo('f1')))!.version).toBe(2);
  });

  it("passes the recovery key's private key on to another admin with an OK, who then signs folders", async () => {
    person('kim', true);
    const k1 = browser('kim', 'k1');
    as(k1);
    await k1.ring.start(k1.me);
    expect(k1.ring.lacksRoot).toBe(true);
    await expect(k1.ring.createFolder('garden')).rejects.toBeInstanceOf(NeedsRoot);
    as(ada);
    await ada.ring.checkIn();
    expect(ada.ring.asks.find((x) => x.id === 'kim')).toEqual(expect.objectContaining({ kind: 'person', root: true }));
    await approve(ada, k1, 'person', 'kim');
    expect(fake.rootGrants.has('kim')).toBe(true);
    as(k1);
    await k1.ring.refresh();
    expect(k1.ring.lacksRoot).toBe(false);
    const garden = await k1.ring.createFolder('garden');
    as(ada);
    await ada.ring.refresh();
    expect(await ada.ring.sendKey(folderInfo(garden.id))).toBeNull();
    // Renamed with a new plain statement, it still sends plain; renamed by the server alone, not.
    as(k1);
    await k1.ring.renameFolder(folderInfo(garden.id), 'allotment');
    as(ada);
    expect(await ada.ring.sendKey(folderInfo(garden.id))).toBeNull();
    fake.folders.get(garden.id)!.name = 'f1';
    await expect(ada.ring.sendKey(folderInfo(garden.id))).rejects.toBeInstanceOf(SendRefused);
  });
});
