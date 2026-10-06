// The keyring's flows against a small fake of the server's keys API (docs/e2ee-plan.md): who
// holds what, what is on whose to-do list, and what each browser can open after its turn.
import { describe, expect, it, vi } from 'vitest';
import type * as Api from '../src/api';
import { fromB64u } from '../src/e2ee/bytes';
import { SealError } from '../src/e2ee/formats';
import { Keyring, keysFromInvite } from '../src/e2ee/keyring';
import { newSeal } from '../src/e2ee/upload';

interface FakePerson {
  admin: boolean;
  publicKey: string | null;
  /** The person's key, sealed for each of their devices. */
  sealed: Map<string, string>;
  lock: string | null;
  folders: Set<string>;
}

const fake = vi.hoisted(() => ({
  caller: null as { user: string; device: string } | { pin: string } | null,
  devices: new Map<string, { user: string; publicKey: string | null }>(),
  people: new Map<string, FakePerson>(),
  /** Each folder's key versions: versions[v - 1] is version v's public key. */
  folders: new Map<string, { encrypted: boolean; versions: string[] }>(),
  /** Folder keys sealed for people, by folder:version:user. */
  sealed: new Map<string, string>(),
  recovery: null as { public_key: string; locked: string } | null,
  recoverySealed: new Map<string, string>(),
  pins: new Map<string, { folder: string; secret_sealed: string; secret_version: number; locked: Map<number, string> }>(),
  rekey: new Set<string>(),
  /** No answer at all. */
  offline: false,
}));

vi.mock('../src/e2ee/store', () => {
  const kept = new Map<string, unknown>();
  return {
    loadDeviceKey: async (id: string) => kept.get(id) ?? null,
    saveDeviceKey: async (k: { deviceId: string }) => void kept.set(k.deviceId, k),
    keepOnly: async () => {},
  };
});

vi.mock('../src/api', async (importOriginal) => {
  const real = await importOriginal<typeof Api>();
  const conflict = (code: string) => new real.ApiError(409, code, code);
  const member = () => {
    const c = fake.caller;
    if (!c || !('user' in c)) throw new real.ApiError(401, 'unauthorized', 'no account');
    return { c, p: fake.people.get(c.user)! };
  };
  const sees = (p: FakePerson, folder: string) => p.admin || p.folders.has(folder);
  const versions = () => [...fake.folders].flatMap(([folder, f]) => f.versions.map((public_key, i) => ({ folder, version: i + 1, public_key })));
  return {
    ...real,
    getKeys: async (): Promise<Api.KeysAnswer> => {
      if (fake.offline) throw new real.ApiError(0, 'network', 'no answer');
      const { c, p } = member();
      const held = (folder: string, version: number, user = c.user) => fake.sealed.has(`${folder}:${version}:${user}`);
      const mine = versions().filter((v) => sees(p, v.folder));
      return {
        device_key: fake.devices.get(c.device)!.publicKey,
        person: { public_key: p.publicKey, sealed: p.sealed.get(c.device) ?? null, password_lock: p.lock },
        folders: mine.map((v) => ({ ...v, sealed: fake.sealed.get(`${v.folder}:${v.version}:${c.user}`) ?? null })),
        recovery_key: p.admin ? (fake.recovery?.public_key ?? null) : null,
        todo: {
          devices: p.publicKey
            ? [...fake.devices].filter(([id, d]) => d.user === c.user && d.publicKey && !p.sealed.has(id)).map(([id, d]) => ({ id, public_key: d.publicKey! }))
            : [],
          people: mine
            .filter((v) => held(v.folder, v.version))
            .flatMap((v) =>
              [...fake.people]
                .filter(([u, q]) => u !== c.user && q.publicKey && sees(q, v.folder) && !held(v.folder, v.version, u))
                .map(([u, q]) => ({ folder: v.folder, version: v.version, user: u, public_key: q.publicKey! })),
            ),
          recovery:
            p.admin && fake.recovery
              ? mine.filter((v) => held(v.folder, v.version) && !fake.recoverySealed.has(`${v.folder}:${v.version}`)).map(({ folder, version }) => ({ folder, version }))
              : [],
          rekey: p.admin ? [...fake.rekey] : [],
          pins: [...fake.pins].flatMap(([pin, x]) =>
            fake.folders
              .get(x.folder)!
              .versions.map((_, i) => i + 1)
              .filter((v) => !x.locked.has(v) && held(x.folder, v) && held(x.folder, x.secret_version))
              .map((version) => ({ pin, folder: x.folder, version, secret_version: x.secret_version, secret_sealed: x.secret_sealed })),
          ),
        },
      };
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
    postGrants: async (g: Api.Grants) => {
      const { c, p } = member();
      for (const d of g.devices) {
        if (fake.devices.get(d.device)?.user !== c.user) throw new real.ApiError(400, 'bad_request', 'not their device');
        p.sealed.set(d.device, d.sealed);
      }
      for (const x of g.people) fake.sealed.set(`${x.folder}:${x.version}:${x.user}`, x.sealed);
      for (const x of g.recovery) fake.recoverySealed.set(`${x.folder}:${x.version}`, x.sealed);
      for (const x of g.pins) fake.pins.get(x.pin)!.locked.set(x.version, x.locked);
    },
    setFolderEncryption: async (folder: string, encrypted: boolean, key?: Api.NewFolderKey) => {
      const { c } = member();
      const f = fake.folders.get(folder)!;
      if (encrypted && !f.versions.length) {
        if (!key) throw conflict('no_key');
        f.versions.push(key.public_key);
        fake.sealed.set(`${folder}:1:${c.user}`, key.sealed);
        fake.recoverySealed.set(`${folder}:1`, key.recovery_sealed);
      }
      f.encrypted = encrypted;
      return { id: folder, encrypted, key_version: f.versions.length || null } as unknown as Api.FolderInfo;
    },
    postFolderKey: async (folder: string, version: number, key: Api.NewFolderKey) => {
      const { c } = member();
      const f = fake.folders.get(folder)!;
      if (version !== f.versions.length + 1) throw conflict('key_outdated');
      f.versions.push(key.public_key);
      fake.sealed.set(`${folder}:${version}:${c.user}`, key.sealed);
      fake.recoverySealed.set(`${folder}:${version}`, key.recovery_sealed);
      fake.rekey.delete(folder);
    },
    putRecovery: async (publicKey: string, locked: string) => {
      member();
      fake.recovery = { public_key: publicKey, locked };
      fake.recoverySealed.clear(); // sealed again for the new key, from the to-do lists
    },
    getRecovery: async (): Promise<Api.Recovery> => {
      member();
      return {
        public_key: fake.recovery?.public_key ?? null,
        locked: fake.recovery?.locked ?? null,
        folders: [...fake.recoverySealed].map(([k, sealed]) => {
          const [folder, v] = k.split(':');
          return { folder, version: Number(v), public_key: fake.folders.get(folder)!.versions[Number(v) - 1], sealed };
        }),
      };
    },
    getPinKeys: async (): Promise<Api.PinKeys> => {
      const c = fake.caller;
      if (!c || !('pin' in c)) throw new real.ApiError(401, 'unauthorized', 'no PIN');
      const x = fake.pins.get(c.pin)!;
      return { folder: x.folder, keys: [...x.locked].map(([version, locked]) => ({ version, public_key: fake.folders.get(x.folder)!.versions[version - 1], locked })) };
    },
  };
});

function person(id: string, admin: boolean, folders: string[] = []) {
  fake.people.set(id, { admin, publicKey: null, sealed: new Map(), lock: null, folders: new Set(folders) });
}

/** A new phone or browser of a person, with its own keyring. */
function browser(user: string, device: string): { ring: Keyring; me: Api.Me } {
  fake.devices.set(device, { user, publicKey: null });
  const me = { user: { id: user, name: user, role: fake.people.get(user)!.admin ? 'admin' : 'member' }, device: { id: device, name: device } } as unknown as Api.Me;
  return { ring: new Keyring(), me };
}

/** Requests from now on come from b. */
function as(b: { me: Api.Me }) {
  fake.caller = { user: b.me.user.id, device: b.me.device.id };
}

const folderInfo = (id: string) => ({ id, encrypted: false, key_version: fake.folders.get(id)!.versions.length || null }) as unknown as Api.FolderInfo;

describe('keyring', { timeout: 60_000 }, () => {
  fake.folders.set('f1', { encrypted: false, versions: [] });
  fake.folders.set('f2', { encrypted: false, versions: [] });
  fake.folders.set('plain', { encrypted: false, versions: [] });
  person('ada', true);
  person('max', false, ['f1']);
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
  });

  it('makes the recovery key, then the first folder key, which seals files', async () => {
    as(a1);
    await expect(a1.ring.encryptFolder(folderInfo('f1'))).rejects.toBeInstanceOf(SealError);
    code = await a1.ring.makeRecovery();
    expect(code).toMatch(/^([0-9A-Z]{4}-){7}[0-9A-Z]{4}$/);
    expect(a1.ring.hasRecovery()).toBe(true);
    await a1.ring.encryptFolder(folderInfo('f1'));
    expect(fake.folders.get('f1')!.versions).toHaveLength(1);
    expect(fake.recoverySealed.has('f1:1')).toBe(true);
    expect(a1.ring.hasFolderKey('f1', 1)).toBe(true);
    const target = a1.ring.encryptFor('f1')!;
    expect(target.version).toBe(1);
    expect(a1.ring.encryptFor('plain')).toBeNull();

    const seal = await newSeal(target, 1000, '1');
    file.enc = { version: 1, key: seal.sealed, header: seal.header };
    fileKey = fromB64u(seal.key);
    expect(await a1.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc })).toEqual(fileKey);
  });

  it("lets another browser wait until one with the key seals it the person's key", async () => {
    as(a2);
    await a2.ring.start(a2.me);
    expect(a2.ring.status).toBe('waiting');
    expect(a2.ring.hasFolderKey('f1', 1)).toBe(false);
    await expect(a2.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).rejects.toBeInstanceOf(SealError);

    as(a1);
    await a1.ring.refresh();
    as(a2);
    await a2.ring.refresh();
    expect(a2.ring.status).toBe('ready');
    expect(await a2.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);
  });

  it('checks in quietly: seals for a browser that waits, which then opens it, never showing loading', async () => {
    const a4 = browser('ada', 'a4');
    as(a4);
    await a4.ring.start(a4.me);
    expect(a4.ring.status).toBe('waiting');
    const seen: string[] = [];
    a4.ring.watch(() => seen.push(a4.ring.status));
    as(a1);
    await a1.ring.checkIn();
    as(a4);
    await a4.ring.checkIn();
    expect(a4.ring.status).toBe('ready');
    expect(seen).not.toContain('loading');
    expect(await a4.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);

    // Without an answer, a check-in keeps what is open, and the status.
    fake.offline = true;
    try {
      await a4.ring.checkIn();
    } finally {
      fake.offline = false;
    }
    expect(a4.ring.status).toBe('ready');
    expect(seen).not.toContain('failed');
    expect(await a4.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);
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
  });

  it('seals a folder key for someone who sees the folder, from an admin', async () => {
    const m1 = browser('max', 'm1');
    as(m1);
    await m1.ring.start(m1.me);
    expect(m1.ring.status).toBe('ready');
    expect(m1.ring.hasFolderKey('f1', 1)).toBe(false);
    as(a1);
    await a1.ring.refresh();
    as(m1);
    await m1.ring.refresh();
    expect(await m1.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);
    // Max doesn't see f2, and gets nothing of it.
    as(a1);
    await a1.ring.encryptFolder(folderInfo('f2'));
    as(m1);
    await m1.ring.refresh();
    expect(m1.ring.encryptFor('f2')).toBeNull();
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
    const keys: Api.InviteKey[] = invite.keys.map((k) => ({ ...k, public_key: fake.folders.get(k.folder)!.versions[k.version - 1] }));

    person('eva', false, ['f1']);
    const e1 = browser('eva', 'e1');
    as(e1);
    await keysFromInvite(e1.me, invite.secret, keys);
    await e1.ring.start(e1.me);
    expect(await e1.ring.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);

    // A link cut before its secret, or with another one, waits for the family.
    person('leo', false, ['f1']);
    const l1 = browser('leo', 'l1');
    as(l1);
    await keysFromInvite(l1.me, (await a1.ring.inviteKeys(['f2'])).secret, keys);
    await l1.ring.start(l1.me);
    expect(l1.ring.status).toBe('ready');
    expect(l1.ring.hasFolderKey('f1', 1)).toBe(false);
  });

  it("brings the person's own key to their new phone through an invite link's secret", async () => {
    as(a1);
    const own = (await a1.ring.personKeyForInvite())!;
    const a5 = browser('ada', 'a5');
    as(a5);
    await keysFromInvite(a5.me, own.secret, [{ folder: null, version: 0, public_key: fake.people.get('ada')!.publicKey!, locked: own.locked }]);
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
      secret_sealed: made.body.sealed,
      secret_version: made.body.version,
      locked: new Map(made.body.keys.map((k) => [k.version, k.locked])),
    });
    const listed = { folder: 'f1', secret: { sealed: made.body.sealed, version: made.body.version } };
    expect(await a1.ring.pinLinkSecret(listed)).toBe(made.secret);
    expect(await a1.ring.pinLinkSecret({ folder: 'f1', secret: null })).toBeNull();
    expect(await a1.ring.pinSecret('plain')).toBeNull();

    fake.caller = { pin: 'p1' };
    const guest = new Keyring();
    expect(await guest.openPinKeys(made.secret)).toBe(1);
    expect(await guest.fileKey({ id: file.id, folder: 'f1', enc: file.enc! })).toEqual(fileKey);
    const stranger = new Keyring();
    expect(await stranger.openPinKeys((await a1.ring.pinSecret('f1'))!.secret)).toBe(0);
  });

  it('makes a new folder key version when asked, which the PIN and the recovery key get too', async () => {
    fake.rekey.add('f1');
    as(a1);
    await a1.ring.refresh();
    expect(fake.folders.get('f1')!.versions).toHaveLength(2);
    expect(a1.ring.encryptFor('f1')!.version).toBe(2);
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
    // It started over with a new person key, which it sealed for the person's other browsers.
    expect(fake.people.get('ada')!.sealed.has('a1')).toBe(true);
    as(a1);
    await a1.ring.refresh();
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
});
