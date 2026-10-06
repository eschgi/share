// This browser's keys for end-to-end encryption (docs/e2ee-plan.md). Its device key stays in
// IndexedDB and is never exported. At every page load the person's key and the folder keys are
// opened again from what the server keeps sealed for this browser, and the to-do list is worked
// through with them: whoever is online with a key seals it for those who lack it.
import * as api from '../api';
import { ApiError, type FileEnc, type KeysAnswer, type Me } from '../api';
import { b64u, fromB64u, randomBytes, type Bytes } from './bytes';
import {
  folderContext,
  generateKeyPair,
  keyPairOf,
  lock,
  newRecoveryCode,
  openKey,
  parseRecoveryCode,
  passwordLock,
  passwordUnlock,
  personContext,
  purposes,
  recoveryContext,
  sealKey,
  SealError,
  secretKey,
  unlock,
  type KeyPair,
} from './formats';
import { keepOnly, loadDeviceKey, saveDeviceKey, type DeviceKey } from './store';

/**
 * Where this browser stands: 'off' before it knows anyone; 'ready' with the person's key
 * open; 'waiting' while no device of the person sealed it for this one (or the person has no
 * key yet and this one couldn't make it); 'failed' when the server or the browser's storage
 * can't be reached.
 */
export type KeysStatus = 'off' | 'loading' | 'ready' | 'waiting' | 'failed';

/** The key to encrypt new files into a folder for: its newest version's public key. */
export interface FolderPublicKey {
  folder: string;
  version: number;
  publicKey: Bytes;
}

const slot = (folder: string, version: number) => `${folder}:${version}`;

/** The device key of this browser for a device id: from IndexedDB, or a new one. */
export async function deviceKeyFor(deviceId: string): Promise<DeviceKey> {
  const kept = await loadDeviceKey(deviceId);
  if (kept) return kept;
  const pair = await generateKeyPair(false);
  const key = { deviceId, privateKey: pair.privateKey, publicKey: pair.publicKey };
  await saveDeviceKey(key);
  return key;
}

/** Whether a private key's scalar belongs to a public key: something sealed for the one opens
 * with the other. */
async function belongs(raw: Bytes, publicKey: Bytes): Promise<boolean> {
  try {
    const pair = await keyPairOf(raw, publicKey);
    const probe = randomBytes(16);
    const sealed = await sealKey(publicKey, purposes.person, probe, probe);
    await openKey(pair, purposes.person, probe, sealed);
    return true;
  } catch {
    return false;
  }
}

export class Keyring {
  status: KeysStatus = 'off';
  me: Me | null = null;
  answer: KeysAnswer | null = null;
  private device: DeviceKey | null = null;
  private person: KeyPair | null = null;
  private folders = new Map<string, KeyPair>();
  private fileKeys = new Map<string, Promise<Bytes>>();
  private listeners = new Set<() => void>();
  private running: Promise<void> | null = null;
  /** How many syncs run now; a check-in skips its turn while one does. */
  private syncing = 0;
  /** The person's key was just opened with the password, which so needs no new lock. */
  private openedWithPassword = false;

  /** Calls f whenever the status or the keys change; returns how to stop. */
  watch(f: () => void): () => void {
    this.listeners.add(f);
    return () => this.listeners.delete(f);
  }

  private changed(status?: KeysStatus) {
    if (status) this.status = status;
    for (const f of this.listeners) f();
  }

  /** Opens this person's keys on this browser, once per page; later calls wait for it. */
  start(me: Me): Promise<void> {
    if (this.me?.device.id !== me.device.id) {
      this.me = me;
      this.running = null;
    }
    this.running ??= this.sync().catch(() => this.changed('failed'));
    return this.running;
  }

  /** Right after signing in with a password, before the library loads: the person's first
   * device makes their key; a new browser opens it with the password; and it stays locked with
   * that password. Never throws; gives up waiting after a while. */
  signedIn(me: Me, password: string): Promise<void> {
    this.me = me;
    return Promise.race([this.refresh(password), new Promise<void>((done) => setTimeout(done, 20_000))]);
  }

  /** Asks the server again and does what is due, e.g. after the folders changed. */
  refresh(password?: string): Promise<void> {
    this.running = this.sync(password).catch(() => this.changed('failed'));
    return this.running;
  }

  /** Checks in with the server: seals the person's key for a phone or browser that waits, and
   * opens what was sealed for this one. Every half minute while the page is shown, and when it
   * comes back; quietly, so the page doesn't flicker through loading, and a check-in without an
   * answer keeps the status. Skipped while another sync runs. */
  checkIn(): Promise<void> {
    if (!this.me || this.syncing > 0) return Promise.resolve();
    this.running = this.sync(undefined, true).catch(() => {});
    return this.running;
  }

  /** Whether the folder's key of that version is open here. */
  hasFolderKey(folder: string, version: number): boolean {
    return this.folders.has(slot(folder, version));
  }

  /** The newest version of a folder's key, to encrypt for; null if it was never encrypted. */
  encryptFor(folder: string): FolderPublicKey | null {
    let best: FolderPublicKey | null = null;
    for (const f of this.answer?.folders ?? []) {
      if (f.folder === folder && (!best || f.version > best.version)) best = { folder, version: f.version, publicKey: fromB64u(f.public_key) };
    }
    return best;
  }

  /** The key of an encrypted file, opened with its folder's key. Throws SealError while that
   * key isn't open here. */
  fileKey(file: { id: string; folder: string; enc: FileEnc }): Promise<Bytes> {
    const id = `${file.id}:${file.folder}:${file.enc.version}:${file.enc.key}`;
    let k = this.fileKeys.get(id);
    if (!k) {
      const pair = this.folders.get(slot(file.folder, file.enc.version));
      if (!pair) return Promise.reject(new SealError("this folder's key isn't open here"));
      k = openKey(pair, purposes.file, folderContext(file.folder, file.enc.version), fromB64u(file.enc.key));
      k.catch(() => this.fileKeys.delete(id));
      this.fileKeys.set(id, k);
    }
    return k;
  }

  private async sync(password?: string, quiet = false): Promise<void> {
    this.syncing++;
    try {
      await this.load(password, quiet);
    } finally {
      this.syncing--;
    }
  }

  private async load(password: string | undefined, quiet: boolean): Promise<void> {
    const me = this.me;
    if (!me) return;
    if (!quiet) this.changed('loading');
    this.device = await deviceKeyFor(me.device.id);
    const device = this.device;
    // Keys of what this browser was before signing in again open nothing any more.
    void keepOnly(me.device.id).catch(() => {});
    let a = await api.getKeys();
    if (a.device_key !== b64u(device.publicKey)) {
      await api.putDeviceKey(b64u(device.publicKey));
      a = await api.getKeys();
    }
    if (!a.person.public_key) {
      // The first device of this person makes their key.
      const p = await generateKeyPair(true);
      const sealed = await sealKey(device.publicKey, purposes.person, personContext(me.user.id), p.raw!);
      try {
        await api.putPersonKey(b64u(p.publicKey), b64u(sealed));
      } catch (e) {
        if (!(e instanceof ApiError && e.code === 'key_exists')) throw e; // another device was quicker
      }
      a = await api.getKeys();
    }
    this.person = await this.openPerson(a, password);
    if (this.person && password && !this.openedWithPassword) await this.keepPasswordLock(a, password);
    // The keys open meanwhile stay usable until the new set replaces them; a version's key
    // never changes, so one already open needn't be opened again.
    const opened = new Map<string, KeyPair>();
    if (this.person) {
      for (const f of a.folders) {
        if (!f.sealed) continue;
        const id = slot(f.folder, f.version);
        const known = this.folders.get(id);
        if (known) {
          opened.set(id, known);
          continue;
        }
        try {
          const raw = await openKey(this.person, purposes.folder, folderContext(f.folder, f.version), fromB64u(f.sealed));
          opened.set(id, await keyPairOf(raw, fromB64u(f.public_key)));
        } catch {
          // sealed for an older key of the person, or broken: the to-do list brings a new one
        }
      }
    }
    this.folders = opened;
    this.answer = a;
    this.changed(this.person ? 'ready' : 'waiting');
    if (this.person && (await this.work(a))) {
      // What was done may have brought new versions to open and seal for others.
      const again = await api.getKeys();
      this.answer = again;
      for (const f of again.folders) {
        if (!f.sealed || this.folders.has(slot(f.folder, f.version))) continue;
        try {
          const raw = await openKey(this.person, purposes.folder, folderContext(f.folder, f.version), fromB64u(f.sealed));
          this.folders.set(slot(f.folder, f.version), await keyPairOf(raw, fromB64u(f.public_key)));
        } catch {
          // as above
        }
      }
      await this.work(again);
      this.changed();
    }
  }

  /** The person's key: sealed for this device, or locked with the password just typed, which
   * then gets sealed for this device too. */
  private async openPerson(a: KeysAnswer, password?: string): Promise<KeyPair | null> {
    const me = this.me!;
    const pub = a.person.public_key ? fromB64u(a.person.public_key) : null;
    this.openedWithPassword = false;
    if (!pub) return null;
    if (a.person.sealed) {
      try {
        const raw = await openKey(this.device!, purposes.person, personContext(me.user.id), fromB64u(a.person.sealed));
        return await keyPairOf(raw, pub);
      } catch {
        // sealed for a key this browser lost: wait, or use the password
      }
    }
    if (password && a.person.password_lock) {
      try {
        const raw = await passwordUnlock(password, personContext(me.user.id), fromB64u(a.person.password_lock));
        if (!(await belongs(raw, pub))) return null;
        const sealed = await sealKey(this.device!.publicKey, purposes.person, personContext(me.user.id), raw);
        await api.postGrants({ devices: [{ device: me.device.id, sealed: b64u(sealed) }], people: [], recovery: [], pins: [] });
        this.openedWithPassword = true;
        return await keyPairOf(raw, pub);
      } catch {
        return null;
      }
    }
    return null;
  }

  /** Keeps the person's key locked with the password just typed, unless that lock is there. */
  private async keepPasswordLock(a: KeysAnswer, password: string): Promise<void> {
    const me = this.me!;
    if (a.person.password_lock) {
      try {
        await passwordUnlock(password, personContext(me.user.id), fromB64u(a.person.password_lock));
        return;
      } catch {
        // a lock with an older password
      }
    }
    await api.putPasswordLock(b64u(await this.passwordLock(password)));
  }

  /** The person's key locked with a password, for a new password. */
  async passwordLock(password: string): Promise<Bytes> {
    if (!this.person?.raw) throw new SealError("the person's key isn't open here");
    return passwordLock(password, personContext(this.me!.user.id), this.person.raw);
  }

  /** Seals and locks what the to-do list asks for; true if it did anything. */
  private async work(a: KeysAnswer): Promise<boolean> {
    const me = this.me!;
    const person = this.person!;
    const g: api.Grants = { devices: [], people: [], recovery: [], pins: [] };
    const raw = (folder: string, version: number) => this.folders.get(slot(folder, version))?.raw;
    for (const d of a.todo.devices) {
      g.devices.push({ device: d.id, sealed: b64u(await sealKey(fromB64u(d.public_key), purposes.person, personContext(me.user.id), person.raw!)) });
    }
    // Each grant carries just its own fields: the server refuses any other (contract/api/keys_grants.json).
    for (const n of a.todo.people) {
      const k = raw(n.folder, n.version);
      if (!k) continue;
      const sealed = await sealKey(fromB64u(n.public_key), purposes.folder, folderContext(n.folder, n.version), k);
      g.people.push({ folder: n.folder, version: n.version, user: n.user, sealed: b64u(sealed) });
    }
    if (a.recovery_key) {
      const recovery = fromB64u(a.recovery_key);
      for (const v of a.todo.recovery) {
        const k = raw(v.folder, v.version);
        if (!k) continue;
        const sealed = await sealKey(recovery, purposes.folder, folderContext(v.folder, v.version), k);
        g.recovery.push({ folder: v.folder, version: v.version, sealed: b64u(sealed) });
      }
    }
    for (const p of a.todo.pins) {
      const holder = this.folders.get(slot(p.folder, p.secret_version));
      const k = raw(p.folder, p.version);
      if (!holder || !k) continue;
      try {
        const secret = await openKey(holder, purposes.pin, folderContext(p.folder, p.secret_version), fromB64u(p.secret_sealed));
        g.pins.push({ pin: p.pin, version: p.version, locked: b64u(await lock(await secretKey(secret, purposes.pin), folderContext(p.folder, p.version), k)) });
      } catch {
        // a broken secret: that PIN's link reads only older files
      }
    }
    let did = false;
    if (g.devices.length || g.people.length || g.recovery.length || g.pins.length) {
      await api.postGrants(g);
      did = true;
    }
    for (const folder of a.todo.rekey) {
      const newest = this.encryptForIn(a, folder);
      if (!newest || !a.recovery_key) continue;
      try {
        await api.postFolderKey(folder, newest.version + 1, await this.newFolderKey(folder, newest.version + 1, fromB64u(a.recovery_key)));
        did = true;
      } catch (e) {
        if (!(e instanceof ApiError && (e.code === 'key_outdated' || e.status === 404))) throw e; // someone else made it
      }
    }
    return did;
  }

  private encryptForIn(a: KeysAnswer, folder: string): FolderPublicKey | null {
    const keep = this.answer;
    this.answer = a;
    try {
      return this.encryptFor(folder);
    } finally {
      this.answer = keep;
    }
  }

  /** A new version of a folder's key, sealed for this person and the recovery key; it is open
   * here from now on. */
  private async newFolderKey(folder: string, version: number, recovery: Bytes): Promise<api.NewFolderKey> {
    const pair = await generateKeyPair(true);
    const aad = folderContext(folder, version);
    const sealed = await sealKey(this.person!.publicKey, purposes.folder, aad, pair.raw!);
    const recoverySealed = await sealKey(recovery, purposes.folder, aad, pair.raw!);
    this.folders.set(slot(folder, version), pair);
    return { public_key: b64u(pair.publicKey), sealed: b64u(sealed), recovery_sealed: b64u(recoverySealed) };
  }

  /** Whether the server has a recovery key yet, which the first encrypted folder needs. */
  hasRecovery(): boolean {
    return !!this.answer?.recovery_key;
  }

  /** Admins: turns encryption on for a folder; the first time with its key's first version. */
  async encryptFolder(folder: api.FolderInfo): Promise<api.FolderInfo> {
    if (folder.key_version) return api.setFolderEncryption(folder.id, true);
    if (!this.person || !this.answer?.recovery_key) throw new SealError('no recovery key yet');
    const key = await this.newFolderKey(folder.id, 1, fromB64u(this.answer.recovery_key));
    const info = await api.setFolderEncryption(folder.id, true, key);
    await this.refresh();
    return info;
  }

  /** Admins: a new recovery key; returns the code, which is shown once. */
  async makeRecovery(): Promise<string> {
    const { secret, code } = newRecoveryCode();
    const pair = await generateKeyPair(true);
    const locked = await lock(await secretKey(secret, purposes.recovery), recoveryContext, pair.raw!);
    await api.putRecovery(b64u(pair.publicKey), b64u(locked));
    await this.refresh();
    return code;
  }

  /** Admins: opens every encrypted folder with the recovery code and keeps its keys for this
   * person; starts over first when this browser has no person key. Throws SealError for a
   * wrong code. */
  async useRecoveryCode(code: string): Promise<number> {
    const secret = parseRecoveryCode(code);
    if (!secret) throw new SealError('not a recovery code');
    const r = await api.getRecovery();
    if (!r.locked || !r.public_key) throw new SealError('no recovery key');
    const raw = await unlock(await secretKey(secret, purposes.recovery), recoveryContext, fromB64u(r.locked));
    const recovery = await keyPairOf(raw, fromB64u(r.public_key));
    if (!this.person) await this.startOver();
    const person = this.person!;
    const people: api.Grants['people'] = [];
    for (const f of r.folders) {
      const aad = folderContext(f.folder, f.version);
      try {
        const folderRaw = await openKey(recovery, purposes.folder, aad, fromB64u(f.sealed));
        people.push({ folder: f.folder, version: f.version, user: this.me!.user.id, sealed: b64u(await sealKey(person.publicKey, purposes.folder, aad, folderRaw)) });
      } catch {
        // sealed for an older recovery key
      }
    }
    if (people.length) await api.postGrants({ devices: [], people, recovery: [], pins: [] });
    await this.refresh();
    return people.length;
  }

  /** Makes a new person key on this browser, when no other device of the person will come:
   * what was sealed for the old one goes, and the others seal again. */
  async startOver(): Promise<void> {
    const me = this.me!;
    this.device ??= await deviceKeyFor(me.device.id);
    const p = await generateKeyPair(true);
    const sealed = await sealKey(this.device.publicKey, purposes.person, personContext(me.user.id), p.raw!);
    await api.putPersonKey(b64u(p.publicKey), b64u(sealed), true);
    this.person = p;
    await this.refresh();
  }

  /** Every version of the keys of these folders (every encrypted one with null), locked with
   * a new secret for an invite's link. */
  async inviteKeys(folders: string[] | null): Promise<{ secret: string; keys: api.LockedFolderKey[] }> {
    const secret = randomBytes(32);
    const key = await secretKey(secret, purposes.invite);
    const keys: api.LockedFolderKey[] = [];
    for (const [id, pair] of this.folders) {
      const [folder, version] = [id.slice(0, id.lastIndexOf(':')), Number(id.slice(id.lastIndexOf(':') + 1))];
      if (folders && !folders.includes(folder)) continue;
      keys.push({ folder, version, locked: b64u(await lock(key, folderContext(folder, version), pair.raw!)) });
    }
    return { secret: b64u(secret), keys };
  }

  /** This person's own key, locked with a new secret for the link of an invite for another of
   * their phones or browsers. */
  async personKeyForInvite(): Promise<{ secret: string; locked: string } | null> {
    if (!this.person?.raw) return null;
    const secret = randomBytes(32);
    const locked = await lock(await secretKey(secret, purposes.invite), personContext(this.me!.user.id), this.person.raw);
    return { secret: b64u(secret), locked: b64u(locked) };
  }

  /** What the link of a PIN that shows an encrypted folder needs: a new secret, sealed for the
   * folder's newest key, and every version of the folder's key locked with it. */
  async pinSecret(folder: string): Promise<{ secret: string; body: api.PinSecret } | null> {
    const newest = this.encryptFor(folder);
    if (!newest) return null;
    const secret = randomBytes(32);
    const key = await secretKey(secret, purposes.pin);
    const sealed = await sealKey(newest.publicKey, purposes.pin, folderContext(folder, newest.version), secret);
    const keys: api.PinSecret['keys'] = [];
    for (const [id, pair] of this.folders) {
      if (!id.startsWith(folder + ':')) continue;
      const version = Number(id.slice(folder.length + 1));
      keys.push({ version, locked: b64u(await lock(key, folderContext(folder, version), pair.raw!)) });
    }
    return { secret: b64u(secret), body: { sealed: b64u(sealed), version: newest.version, keys } };
  }

  /** The secret of the link of a PIN that shows an encrypted folder, opened with the folder's
   * key; null when there is none, or that key isn't open here. */
  async pinLinkSecret(pin: { folder: string; secret: { sealed: string; version: number } | null }): Promise<string | null> {
    const pair = pin.secret && this.folders.get(slot(pin.folder, pin.secret.version));
    if (!pin.secret || !pair) return null;
    try {
      return b64u(await openKey(pair, purposes.pin, folderContext(pin.folder, pin.secret.version), fromB64u(pin.secret.sealed)));
    } catch {
      return null;
    }
  }

  /** Admins moving encrypted files: their keys, sealed for the target folder's newest key. */
  async moveKeys(files: { id: string; folder: string; enc: FileEnc | null }[], target: string): Promise<api.MovedKey[]> {
    const newest = this.encryptFor(target);
    const out: api.MovedKey[] = [];
    for (const f of files) {
      if (!f.enc || f.folder === target) continue;
      if (!newest) throw new SealError('the folder was never encrypted');
      const key = await this.fileKey({ id: f.id, folder: f.folder, enc: f.enc });
      const sealed = await sealKey(newest.publicKey, purposes.file, folderContext(target, newest.version), key);
      out.push({ id: f.id, version: newest.version, key: b64u(sealed) });
    }
    return out;
  }

  /** A PIN guest's keys: the folder's keys locked with the secret of the PIN's link. */
  async openPinKeys(secret: string): Promise<number> {
    const k = await api.getPinKeys();
    const key = await secretKey(fromB64u(secret), purposes.pin);
    for (const v of k.keys) {
      try {
        const raw = await unlock(key, folderContext(k.folder, v.version), fromB64u(v.locked));
        this.folders.set(slot(k.folder, v.version), await keyPairOf(raw, fromB64u(v.public_key)));
      } catch {
        // locked with another secret
      }
    }
    this.changed('ready');
    return this.folders.size;
  }
}

/** The keys of this page. */
export const keyring = new Keyring();

/** Sets up the keys of a browser that just accepted an invite, with the keys its link's secret
 * unlocks: the person's own (a new phone or browser of someone), or folder keys for a new
 * person, which get sealed for their new person key. Without a secret, or with a wrong one,
 * the browser waits for the others like any new one. */
export async function keysFromInvite(me: Me, secret: string | null, keys: api.InviteKey[]): Promise<void> {
  const device = await deviceKeyFor(me.device.id);
  await api.putDeviceKey(b64u(device.publicKey));
  if (!secret) return;
  const key = await secretKey(fromB64u(secret), purposes.invite);
  const own = keys.find((k) => k.folder === null);
  let person: KeyPair;
  if (own) {
    const raw = await unlock(key, personContext(me.user.id), fromB64u(own.locked));
    person = await keyPairOf(raw, fromB64u(own.public_key));
    const sealed = await sealKey(device.publicKey, purposes.person, personContext(me.user.id), raw);
    await api.postGrants({ devices: [{ device: me.device.id, sealed: b64u(sealed) }], people: [], recovery: [], pins: [] });
    return;
  }
  person = await generateKeyPair(true);
  const sealed = await sealKey(device.publicKey, purposes.person, personContext(me.user.id), person.raw!);
  try {
    await api.putPersonKey(b64u(person.publicKey), b64u(sealed));
  } catch (e) {
    if (e instanceof ApiError && e.code === 'key_exists') return; // someone joining twice: the key there stays
    throw e;
  }
  const people: api.Grants['people'] = [];
  for (const k of keys) {
    if (!k.folder) continue;
    const aad = folderContext(k.folder, k.version);
    try {
      const raw = await unlock(key, aad, fromB64u(k.locked));
      people.push({ folder: k.folder, version: k.version, user: me.user.id, sealed: b64u(await sealKey(person.publicKey, purposes.folder, aad, raw)) });
    } catch {
      // locked with another secret
    }
  }
  if (people.length) await api.postGrants({ devices: [], people, recovery: [], pins: [] });
}
