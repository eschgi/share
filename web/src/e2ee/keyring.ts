// This browser's keys for end-to-end encryption (docs/e2ee-plan.md). Its device key stays in
// IndexedDB and is never exported. At every page load the person's key and the folder keys are
// opened again from what the server keeps sealed for this browser, and the to-do list is worked
// through with them: whoever is online with a key seals it for those who lack it, after a check
// whose code the person here compares, unless it is a person whose key was checked here before.
// What the server says about keys counts only as far as the root this browser trusts signed it,
// and this browser keeps what it saw (store.ts: pins), so a changed database can't take it back.
import * as api from '../api';
import { ApiError, type FileEnc, type FolderInfo, type KeysAnswer, type Me, type RootInfo } from '../api';
import { b64u, equalBytes, fromB64u, randomBytes, utf8, type Bytes } from './bytes';
import {
  checkCode,
  checkContext,
  commitment,
  confirmKey,
  fingerprint,
  folderContext,
  folderKeyMessage,
  generateKeyPair,
  keyPairOf,
  lock,
  newRecoveryCode,
  noteContext,
  noteKey,
  oneTimeKey,
  openKey,
  parseRecoveryCode,
  passwordLock,
  passwordUnlock,
  personContext,
  pinSecretKey,
  plainMessage,
  purposes,
  recoveryContext,
  rootContext,
  rootMessage,
  sealKey,
  SealError,
  secretKey,
  sign,
  unlock,
  verify,
  type KeyPair,
} from './formats';
import { keepOnly, loadDeviceKey, saveDeviceKey, savePins, saveTrusted, type DeviceKey, type Pins } from './store';
import { canEncrypt, follow, SendRefused, type FolderPublicKey } from './trust';

/**
 * Where this browser stands: 'off' before it knows anyone; 'ready' with the person's key
 * open; 'waiting' while no device of the person sealed it for this one (or the person has no
 * key yet and this one couldn't make it); 'failed' when the server or the browser's storage
 * can't be reached; 'insecure' on a page opened over plain http, where browsers don't encrypt.
 */
export type KeysStatus = 'off' | 'loading' | 'ready' | 'waiting' | 'failed' | 'insecure';

export { SendRefused, type FolderPublicKey } from './trust';

/** Changing a folder needs the root's private key, which this browser doesn't hold (yet): another
 * admin passes it on with an OK. */
export class NeedsRoot extends Error {}

const slot = (folder: string, version: number) => `${folder}:${version}`;

/**
 * Someone this browser would pass keys on to once the person here allows it, after both screens
 * showed the same code (docs/e2ee-plan.md): a device of the person that waits for their key, or
 * another person who waits for folder keys, or as an admin for the root's private key, and whose
 * key wasn't checked here before. Only those seen lately, who can answer. The library lists them;
 * Show opens one, which starts its check: code is null until the other side answered.
 */
export interface Ask {
  kind: 'device' | 'person';
  /** The device's id, or the person's. */
  id: string;
  name: string;
  /** A device: a phone (app) or a browser (web), and when it signed in. */
  client?: 'app' | 'web';
  since?: string;
  /** A person: the folders they wait for, and whether they get the root's private key too. */
  folders?: string[];
  root?: boolean;
  code: string | null;
}

/** A code this browser shows for a check another device asks: who asks, and the code. */
export interface ShownCode {
  kind: 'device' | 'person';
  from: string;
  code: string;
}

/** A check this browser asks: its id, its one-time key, the key it checks, and the answer it
 * revealed its key for, which the code is made from; null before. */
interface Asking {
  check: string;
  mine: { privateKey: CryptoKey; publicKey: Bytes };
  key: Bytes;
  answer: Bytes | null;
}

/** A check this browser answers: its one-time key, and the commitment it saw before answering,
 * which the revealed key must match. */
interface Answering {
  mine: { privateKey: CryptoKey; publicKey: Bytes };
  commitment: Bytes;
}

/** What a check's confirmation hands on (contract/api/keys_check_confirm.json). */
interface Confirmed {
  root: string | null;
  person?: { public_key: string; private_key: string };
}

/** The person's note: the root their phones and browsers trust, and the newest version of each
 * folder's key they saw. */
interface Note {
  root: string | null;
  folders: Record<string, number>;
}

/** The device key of this browser for a device id: from IndexedDB, or a new one. */
export async function deviceKeyFor(deviceId: string): Promise<DeviceKey> {
  const kept = await loadDeviceKey(deviceId);
  if (kept) return kept;
  return newDeviceKey(deviceId);
}

async function newDeviceKey(deviceId: string): Promise<DeviceKey> {
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
  /** Who this browser would pass keys on to, after a check: listed, until the person here opens
   * one with Show. */
  asks: Ask[] = [];
  /** The codes this browser shows for checks other devices ask. */
  codes: ShownCode[] = [];
  private asking = new Map<string, Asking>();
  /** The checks this browser answers, by check id. */
  private answering = new Map<string, Answering>();
  /** The asks the person here opened with Show, kind:id: only those get a check. */
  private shown = new Set<string>();
  /** The people's keys checked here: user id to key. */
  private trusted: Record<string, string> = {};
  /** What this browser keeps of the keys (store.ts). */
  private pins: Pins = { folders: {} };
  /** The root this browser trusts: the newest the chain leads to from the one it learned. */
  private root: Bytes | null = null;
  /** The root's private key, for an admin who holds it. */
  private rootKey: KeyPair | null = null;
  /** The person's note, as the server has it. */
  private note: Note | null = null;
  /** Signatures checked here, and what came of it. */
  private verified = new Map<string, boolean>();
  /** A password typed on this page while the person's key wasn't open here: it locks the key once
   * the key opens (another device sealed it, or this one made a new one), so that the next device
   * opens it with the password. Only ever in memory. */
  private typedPassword: string | null = null;
  /** A PIN guest's root, opened with the secret of a PIN link that shows its folder. */
  guestRoot: Bytes | null = null;

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
      this.typedPassword = null;
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

  /** Whether publicKey signed message: checked once per page. */
  private async signed(publicKey: Bytes | null, message: Bytes, signature: string | null): Promise<boolean> {
    if (!publicKey || !signature) return false;
    const id = `${b64u(publicKey)}:${b64u(message)}:${signature}`;
    let ok = this.verified.get(id);
    if (ok === undefined) {
      ok = await verify(publicKey, message, fromB64u(signature));
      this.verified.set(id, ok);
    }
    return ok;
  }

  /** Whether the root this browser trusts signed that version of a folder's key. */
  private signedKey(f: KeysAnswer['folders'][number]): Promise<boolean> {
    return this.signed(this.root, folderKeyMessage(f.folder, f.version, fromB64u(f.public_key)), f.signature);
  }

  private async sync(password?: string, quiet = false): Promise<void> {
    if (!canEncrypt()) return this.changed('insecure');
    this.syncing++;
    try {
      // A check's confirmation brings this browser the person's key or the root: the rest
      // opens with them right away.
      if (await this.load(password, quiet)) await this.load(undefined, true);
    } finally {
      this.syncing--;
    }
  }

  /** One round with the server; true if a check's confirmation brought something new. */
  private async load(password: string | undefined, quiet: boolean): Promise<boolean> {
    const me = this.me;
    if (!me) return false;
    if (!quiet) this.changed('loading');
    this.device = await deviceKeyFor(me.device.id);
    this.trusted = this.device.trusted ?? {};
    this.pins = this.device.pins ?? { folders: {} };
    const kept = JSON.stringify(this.pins);
    // Keys of what this browser was before signing in again open nothing any more.
    void keepOnly(me.device.id).catch(() => {});
    let a = await api.getKeys();
    if (a.device_key !== b64u(this.device.publicKey)) {
      await api.putDeviceKey(b64u(this.device.publicKey));
      a = await api.getKeys();
    }
    // The first device of this person makes their key, and so does a member's device once their
    // key is lost: no phone or browser holds it any more, and no password opens it. Nothing is lost
    // with a new one: whoever has Share open with the folders seals them for it again. An admin's
    // device asks instead, as the recovery code opens every folder again.
    const lost = !!a.person.public_key && a.person.held_by === 0 && !a.person.password_lock && me.user.role !== 'admin';
    if (!a.person.public_key || lost) {
      const p = await generateKeyPair(true);
      const sealed = await sealKey(this.device.publicKey, purposes.person, personContext(me.user.id), p.raw!);
      try {
        await api.putPersonKey(b64u(p.publicKey), b64u(sealed), lost);
        this.pins = { ...this.pins, person: b64u(p.publicKey) };
        await savePins(me.device.id, this.pins);
      } catch (e) {
        if (!(e instanceof ApiError && e.code === 'key_exists')) throw e; // another device was quicker
      }
      a = await api.getKeys();
    }
    this.person = await this.openPerson(a, password);
    if (!this.person && a.person.sealed && a.person.public_key && !password) {
      // Sealed for this browser, but not a key it was given where a database can't change it: a
      // new device key drops it, so that this browser is asked for again, with a check.
      this.device = await newDeviceKey(me.device.id);
      this.pins = { ...this.pins, person: undefined };
      await savePins(me.device.id, this.pins);
      await api.putDeviceKey(b64u(this.device.publicKey));
      a = await api.getKeys();
    }
    if (password) this.typedPassword = password;
    if (this.person && this.typedPassword && !this.openedWithPassword) await this.keepPasswordLock(a, this.typedPassword);
    if (this.person) this.typedPassword = null;
    this.note = await this.openNote(a);
    await this.learnRoot(a);
    this.rootKey = await this.openRoot(a);
    // The keys open meanwhile stay usable until the new set replaces them; a version's key
    // never changes, so one already open needn't be opened again. Only those the root signed,
    // whose private key belongs to the signed public key.
    const opened = new Map<string, KeyPair>();
    if (this.person) {
      for (const f of a.folders) {
        if (!f.sealed) continue;
        const id = slot(f.folder, f.version);
        const known = this.folders.get(id);
        if (known && equalBytes(known.publicKey, fromB64u(f.public_key))) {
          opened.set(id, known);
          continue;
        }
        const pair = await this.openFolderKey(f);
        if (pair) opened.set(id, pair);
      }
    }
    this.folders = opened;
    this.answer = a;
    const confirmed = await this.answerChecks(a);
    if (JSON.stringify(this.pins) !== kept) await savePins(me.device.id, this.pins);
    if (this.person) await this.keepNote().catch(() => {});
    this.changed(this.person ? 'ready' : 'waiting');
    if (confirmed) return true;
    if (!this.person) return false;
    const asked = JSON.stringify(this.asks);
    const did = await this.work(a);
    if (did) {
      // What was done may have brought new versions to open and seal for others.
      const again = await api.getKeys();
      this.answer = again;
      for (const f of again.folders) {
        if (!f.sealed || this.folders.has(slot(f.folder, f.version))) continue;
        const pair = await this.openFolderKey(f);
        if (pair) this.folders.set(slot(f.folder, f.version), pair);
      }
      await this.work(again);
    }
    if (did || JSON.stringify(this.asks) !== asked) this.changed();
    return false;
  }

  /** A version of a folder's key sealed for the person: null unless the root signed it and the
   * private key belongs to it. */
  private async openFolderKey(f: KeysAnswer['folders'][number]): Promise<KeyPair | null> {
    if (!this.person || !f.sealed || !(await this.signedKey(f))) return null;
    try {
      const pub = fromB64u(f.public_key);
      const raw = await openKey(this.person, purposes.folder, folderContext(f.folder, f.version), fromB64u(f.sealed));
      return (await belongs(raw, pub)) ? await keyPairOf(raw, pub) : null;
    } catch {
      return null; // sealed for an older key of the person, or broken: the to-do list brings a new one
    }
  }

  /** The person's key: sealed for this device, if it is the key this browser keeps; or locked
   * with the password just typed, which vouches for it, and then sealed for this device too. */
  private async openPerson(a: KeysAnswer, password?: string): Promise<KeyPair | null> {
    const me = this.me!;
    const pub = a.person.public_key ? fromB64u(a.person.public_key) : null;
    this.openedWithPassword = false;
    if (!pub) return null;
    if (a.person.sealed && this.pins.person === a.person.public_key) {
      try {
        const raw = await openKey(this.device!, purposes.person, personContext(me.user.id), fromB64u(a.person.sealed));
        // Checks show codes made from this key, so the key the server names must be this one's.
        if (await belongs(raw, pub)) return await keyPairOf(raw, pub);
      } catch {
        // sealed for a key this browser lost: wait, or use the password
      }
    }
    if (password && a.person.password_lock) {
      try {
        const raw = await passwordUnlock(password, personContext(me.user.id), fromB64u(a.person.password_lock));
        if (!(await belongs(raw, pub))) return null;
        this.pins = { ...this.pins, person: b64u(pub) };
        await savePins(me.device.id, this.pins);
        const sealed = await sealKey(this.device!.publicKey, purposes.person, personContext(me.user.id), raw);
        await api.postGrants({ ...noGrants(), devices: [{ device: me.device.id, sealed: b64u(sealed) }] });
        this.openedWithPassword = true;
        return await keyPairOf(raw, pub);
      } catch {
        return null;
      }
    }
    return null;
  }

  /** The person's note, opened with their key; null without one, or one that doesn't open. */
  private async openNote(a: KeysAnswer): Promise<Note | null> {
    if (!this.person?.raw || !a.person.note) return null;
    try {
      const plain = await unlock(await noteKey(this.person.raw), noteContext(this.me!.user.id), fromB64u(a.person.note));
      const n = JSON.parse(new TextDecoder().decode(plain)) as Partial<Note>;
      const folders: Record<string, number> = {};
      for (const [f, v] of Object.entries(n.folders ?? {})) if (Number.isInteger(v) && v > 0) folders[f] = v;
      return { root: typeof n.root === 'string' ? n.root : null, folders };
    } catch {
      return null;
    }
  }

  /** The root this browser trusts: the one it keeps, or else the person's note's, or else the
   * newest the server names (tofu, which the note's replaces); then the newest the chain leads
   * to from it. And the newest version of each folder's key it signed, kept with the note's. */
  private async learnRoot(a: KeysAnswer): Promise<void> {
    let root = this.pins.root ? fromB64u(this.pins.root) : null;
    let tofu = !!this.pins.tofu;
    const noted = this.note?.root ? fromB64u(this.note.root) : null;
    if (noted && (!root || (tofu && !equalBytes(noted, root)))) [root, tofu] = [noted, false];
    const newest = a.roots.at(-1);
    if (!root && newest) [root, tofu] = [fromB64u(newest.public_key), true];
    this.root = root && (await follow(a.roots, root));
    const folders = { ...this.pins.folders };
    for (const [f, v] of Object.entries(this.note?.folders ?? {})) folders[f] = Math.max(folders[f] ?? 0, v);
    for (const f of a.folders) if (await this.signedKey(f)) folders[f.folder] = Math.max(folders[f.folder] ?? 0, f.version);
    this.pins = { ...this.pins, root: this.root ? b64u(this.root) : undefined, tofu: (this.root && tofu) || undefined, folders };
  }

  /** Trusts a root that came where a database can't change it: made here, the recovery code, an
   * invite's link, a check. */
  private async adoptRoot(root: Bytes, roots: RootInfo[]): Promise<void> {
    this.root = await follow(roots, root);
    this.pins = { ...this.pins, root: b64u(this.root), tofu: undefined };
    if (this.me) await savePins(this.me.device.id, this.pins);
  }

  /** The newest root's private key, sealed for the person: an admin's, when it is the root this
   * browser trusts. */
  private async openRoot(a: KeysAnswer): Promise<KeyPair | null> {
    if (!this.person || !this.root || !a.root_sealed) return null;
    if (this.rootKey && equalBytes(this.rootKey.publicKey, this.root)) return this.rootKey;
    try {
      const raw = await openKey(this.person, purposes.root, rootContext, fromB64u(a.root_sealed));
      return (await belongs(raw, this.root)) ? await keyPairOf(raw, this.root) : null;
    } catch {
      return null;
    }
  }

  /** Writes what this browser keeps into the person's note, where the note lacks it. */
  private async keepNote(): Promise<void> {
    if (!this.person?.raw || !this.root || this.pins.tofu) return;
    const n = this.note;
    const root = b64u(this.root);
    const folders = { ...(n?.folders ?? {}) };
    let stale = n?.root !== root;
    for (const [f, v] of Object.entries(this.pins.folders)) {
      if ((folders[f] ?? 0) < v) {
        folders[f] = v;
        stale = true;
      }
    }
    if (!stale) return;
    const note: Note = { root, folders };
    const locked = await lock(await noteKey(this.person.raw), noteContext(this.me!.user.id), utf8(JSON.stringify(note)));
    await api.putNote(b64u(locked));
    this.note = note;
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

  /** Seals and locks what the to-do list asks for, and asks first where a check is due; true if
   * it did anything. */
  private async work(a: KeysAnswer): Promise<boolean> {
    const g = noGrants();
    const raw = (folder: string, version: number) => this.folders.get(slot(folder, version))?.raw;
    // The person's other devices get their key only after a check, once the person sees an
    // encrypted folder: where none is, nobody is asked. So do people whose key wasn't checked
    // here before. A check needs someone to answer it: only those seen lately are asked. This
    // browser isn't, when it opened the key with the password after the list was made. Each
    // grant carries just its own fields: the server refuses any other
    // (contract/api/keys_grants.json).
    const asks: Ask[] = a.todo.devices
      .filter((d) => a.folders.length && d.active && d.id !== this.me!.device.id)
      .map((d) => ({ kind: 'device', id: d.id, name: d.name, client: d.client, since: d.created_at, code: null }));
    const people = new Map<string, Ask>();
    const ask = (user: string, name: string) => {
      const x = people.get(user) ?? { kind: 'person', id: user, name, folders: [], code: null };
      people.set(user, x);
      return x;
    };
    for (const n of a.todo.people) {
      const k = raw(n.folder, n.version);
      if (!k) continue;
      if (this.trusted[n.user] === n.public_key) {
        const sealed = await sealKey(fromB64u(n.public_key), purposes.folder, folderContext(n.folder, n.version), k);
        g.people.push({ folder: n.folder, version: n.version, user: n.user, sealed: b64u(sealed) });
        continue;
      }
      if (!n.active) continue;
      const x = ask(n.user, n.name);
      if (!x.folders!.includes(n.folder)) x.folders!.push(n.folder);
    }
    // Admins get the root's private key the same way, from an admin who holds it.
    for (const n of this.rootKey ? a.todo.roots : []) {
      if (this.trusted[n.user] === n.public_key) {
        g.roots.push({ user: n.user, sealed: b64u(await sealKey(fromB64u(n.public_key), purposes.root, rootContext, this.rootKey!.raw!)) });
        continue;
      }
      if (n.active) ask(n.user, n.name).root = true;
    }
    asks.push(...people.values());
    this.asks = await this.askChecks(a, asks);
    // Sealed for the recovery key only when it is the root this browser trusts: a database that
    // names another one gets nothing.
    const newest = a.roots.at(-1)?.public_key;
    if (this.root && newest === b64u(this.root)) {
      for (const v of a.todo.recovery) {
        const k = raw(v.folder, v.version);
        if (!k) continue;
        const sealed = await sealKey(this.root, purposes.folder, folderContext(v.folder, v.version), k);
        g.recovery.push({ folder: v.folder, version: v.version, sealed: b64u(sealed) });
      }
    }
    // Versions locked for a PIN's link only for a secret that someone who holds the folder's key
    // locked: it opens with a key from that.
    for (const p of a.todo.pins) {
      const holder = raw(p.folder, p.secret_version);
      const k = raw(p.folder, p.version);
      if (!holder || !k) continue;
      try {
        const secret = await unlock(await pinSecretKey(holder), folderContext(p.folder, p.secret_version), fromB64u(p.secret_locked));
        g.pins.push({ pin: p.pin, version: p.version, locked: b64u(await lock(await secretKey(secret, purposes.pin), folderContext(p.folder, p.version), k)) });
      } catch {
        // a secret nobody with the folder's key made: that PIN's link gets nothing
      }
    }
    let did = false;
    if (g.devices.length || g.people.length || g.recovery.length || g.pins.length || g.roots.length) {
      await api.postGrants(g);
      did = true;
    }
    for (const folder of this.rootKey ? a.todo.rekey : []) {
      const version = Math.max(0, ...a.folders.filter((f) => f.folder === folder).map((f) => f.version));
      if (!version) continue;
      try {
        await api.postFolderKey(folder, version + 1, await this.newFolderKey(folder, version + 1));
        did = true;
      } catch (e) {
        if (!(e instanceof ApiError && (e.code === 'key_outdated' || e.code === 'not_encrypted' || e.status === 404))) throw e; // someone else made it
      }
    }
    return did;
  }

  /** The checks this browser asks: one is opened for every ask the person opened with Show, its
   * one-time key revealed once the other side answered, and the code made then, from that answer:
   * one the server shows later can't change it. Checks of asks closed or gone are closed too.
   * Returns the asks still due. */
  private async askChecks(a: KeysAnswer, asks: Ask[]): Promise<Ask[]> {
    const keyOf = (ask: Ask) =>
      fromB64u(
        ask.kind === 'device'
          ? a.todo.devices.find((d) => d.id === ask.id)!.public_key
          : (a.todo.people.find((n) => n.user === ask.id) ?? a.todo.roots.find((n) => n.user === ask.id))!.public_key,
      );
    const due = new Set(asks.map((x) => `${x.kind}:${x.id}`));
    for (const k of [...this.shown]) if (!due.has(k)) this.shown.delete(k);
    for (const [k, c] of this.asking) {
      if (this.shown.has(k)) continue;
      this.asking.delete(k);
      void api.closeCheck(c.check).catch(() => {});
    }
    const out: Ask[] = [];
    for (const ask of asks) {
      const k = `${ask.kind}:${ask.id}`;
      if (!this.shown.has(k)) {
        out.push(ask);
        continue;
      }
      const key = keyOf(ask);
      let c = this.asking.get(k);
      const open = c && a.checks.find((x) => x.asking && x.id === c!.check);
      if (c && (!open || !equalBytes(c.key, key))) {
        // closed, out of time, or another key now: a new check
        if (open) void api.closeCheck(c.check).catch(() => {});
        this.asking.delete(k);
        c = undefined;
      }
      try {
        if (!c) {
          const mine = await oneTimeKey();
          const { id } = await api.openCheck(ask.kind === 'device' ? { device: ask.id } : { user: ask.id }, b64u(await commitment(mine.publicKey)));
          this.asking.set(k, { check: id, mine, key, answer: null });
        } else if (!c.answer && open?.answer) {
          await api.revealCheck(c.check, b64u(c.mine.publicKey), open.answer);
          c.answer = fromB64u(open.answer);
        }
      } catch (e) {
        if (!(e instanceof ApiError)) throw e;
        if (!c && e.status === 404) {
          this.shown.delete(k); // nobody waits there any more
          continue;
        }
        if (e.status === 404) this.asking.delete(k); // the check is gone: a new one next time
        else if (e.status !== 409) throw e; // 409: answered anew meanwhile; revealed for that next time
      }
      if (c?.answer) ask.code = await checkCode(c.mine.publicKey, c.answer, c.key);
      out.push(ask);
    }
    return out;
  }

  /** The checks other devices ask of this one: answered with a one-time key of its own, and the
   * code shown once the asking device revealed the key it committed to before this answer. A
   * device check is made from this browser's own key; a person's from the person's key, which
   * must be open here. A confirmation, which only this browser opens, brings the root, and to a
   * device of the person their key; true when it brought something. */
  private async answerChecks(a: KeysAnswer): Promise<boolean> {
    const codes: ShownCode[] = [];
    const listed = new Set<string>();
    let brought = false;
    for (const c of a.checks) {
      if (c.asking) continue;
      const kind = c.device ? 'device' : 'person';
      const key = kind === 'device' ? this.device?.publicKey : this.person?.publicKey;
      if (!key) continue;
      listed.add(c.id);
      const mine = this.answering.get(c.id);
      if (!mine || !c.answered) {
        try {
          if (c.reveal) {
            // revealed for a key this page no longer has: closed, so that a new check starts
            await api.closeCheck(c.id);
          } else {
            const ot = await oneTimeKey();
            await api.answerCheck(c.id, b64u(ot.publicKey));
            this.answering.set(c.id, { mine: ot, commitment: fromB64u(c.commitment) });
          }
        } catch (e) {
          if (!(e instanceof ApiError && e.status === 404)) throw e; // closed, or another device of the person answered
        }
        continue;
      }
      if (!c.reveal) continue;
      const revealed = fromB64u(c.reveal);
      if (!equalBytes(await commitment(revealed), mine.commitment)) continue; // not the key committed to
      if (!c.confirmation) {
        codes.push({ kind, from: c.from, code: await checkCode(revealed, mine.mine.publicKey, key) });
        continue;
      }
      // Allowed on the other side: what it hands on opens only with this side's one-time key.
      try {
        const k = await confirmKey(mine.mine.privateKey, revealed, revealed, mine.mine.publicKey, key);
        const got = JSON.parse(new TextDecoder().decode(await unlock(k, checkContext(c.id), fromB64u(c.confirmation)))) as Confirmed;
        brought = (await this.takeConfirmed(a, kind, got)) || brought;
      } catch {
        // not for this side's key: nothing to take
      }
      this.answering.delete(c.id);
      await api.closeCheck(c.id).catch(() => {});
    }
    for (const id of [...this.answering.keys()]) if (!listed.has(id)) this.answering.delete(id);
    this.codes = codes;
    return brought;
  }

  /** Takes what a check's confirmation brought: the person's key for this device, sealed for
   * itself, and the root. */
  private async takeConfirmed(a: KeysAnswer, kind: 'device' | 'person', got: Confirmed): Promise<boolean> {
    const me = this.me!;
    let brought = false;
    if (kind === 'device' && got.person) {
      const pub = fromB64u(got.person.public_key);
      const raw = fromB64u(got.person.private_key);
      if (a.person.public_key === got.person.public_key && (await belongs(raw, pub))) {
        this.pins = { ...this.pins, person: got.person.public_key };
        await savePins(me.device.id, this.pins);
        const sealed = await sealKey(this.device!.publicKey, purposes.person, personContext(me.user.id), raw);
        await api.postGrants({ ...noGrants(), devices: [{ device: me.device.id, sealed: b64u(sealed) }] });
        this.person = await keyPairOf(raw, pub);
        brought = true;
      }
    }
    if (got.root && (!this.root || this.pins.tofu || got.root !== b64u(this.root))) {
      const root = fromB64u(got.root);
      // A root the chain leads to from this one is older: this browser keeps the newer one.
      if (!this.root || this.pins.tofu || !equalBytes(await follow(a.roots, root), this.root)) {
        await this.adoptRoot(root, a.roots);
        brought = true;
      }
    }
    return brought;
  }

  /** Allows an ask after the person here compared the code: hands the person's key on to the
   * device in the check's confirmation, or seals the folder keys, and for an admin the root's
   * private key, for the person, whose key counts as checked here from now on; and hands on the
   * root this browser trusts. */
  async allow(ask: Ask): Promise<void> {
    const me = this.me!;
    const c = this.asking.get(`${ask.kind}:${ask.id}`);
    if (!c?.answer || !this.person?.raw || !this.answer) throw new SealError('no code to compare yet');
    const confirmed: Confirmed = { root: this.root && !this.pins.tofu ? b64u(this.root) : null };
    if (ask.kind === 'device') {
      confirmed.person = { public_key: b64u(this.person.publicKey), private_key: b64u(this.person.raw) };
    } else {
      const g = noGrants();
      for (const n of this.answer.todo.people) {
        const k = this.folders.get(slot(n.folder, n.version))?.raw;
        if (n.user !== ask.id || !k || !equalBytes(fromB64u(n.public_key), c.key)) continue;
        const sealed = await sealKey(c.key, purposes.folder, folderContext(n.folder, n.version), k);
        g.people.push({ folder: n.folder, version: n.version, user: n.user, sealed: b64u(sealed) });
      }
      const root = this.answer.todo.roots.find((n) => n.user === ask.id && equalBytes(fromB64u(n.public_key), c.key));
      if (root && this.rootKey) g.roots.push({ user: ask.id, sealed: b64u(await sealKey(c.key, purposes.root, rootContext, this.rootKey.raw!)) });
      await api.postGrants(g);
      this.trusted = { ...this.trusted, [ask.id]: b64u(c.key) };
      await saveTrusted(me.device.id, this.trusted);
    }
    const k = await confirmKey(c.mine.privateKey, c.answer, c.mine.publicKey, c.answer, c.key);
    await api.confirmCheck(c.check, b64u(await lock(k, checkContext(c.check), utf8(JSON.stringify(confirmed)))));
    // The waiting side closes the check once it read the confirmation.
    this.shown.delete(`${ask.kind}:${ask.id}`);
    this.asking.delete(`${ask.kind}:${ask.id}`);
    this.asks = this.asks.filter((x) => !(x.kind === ask.kind && x.id === ask.id));
    this.changed();
    await this.checkIn();
  }

  /** Opens an ask (Show): its check starts with the check-in this returns, and its code comes
   * once the other side answered. */
  show(ask: Ask): Promise<void> {
    this.shown.add(`${ask.kind}:${ask.id}`);
    this.changed();
    return this.checkIn();
  }

  /** The ask whose dialog is open: the one the person opened with Show, while it is due. */
  get opened(): Ask | null {
    return this.asks.find((x) => this.shown.has(`${x.kind}:${x.id}`)) ?? null;
  }

  /** Closes an ask's dialog ("Not now"): its check ends, and the ask stays listed. */
  async hide(ask: Ask): Promise<void> {
    const k = `${ask.kind}:${ask.id}`;
    const c = this.asking.get(k);
    this.shown.delete(k);
    this.asking.delete(k);
    this.asks = this.asks.map((x) => (`${x.kind}:${x.id}` === k ? { ...x, code: null } : x));
    this.changed();
    if (c) await api.closeCheck(c.check).catch(() => {});
  }

  /** Not me: a phone or browser that asked for the person's key, and isn't theirs, is signed out. */
  async deny(ask: Ask): Promise<void> {
    await api.signOutDevice(ask.id);
    await this.hide(ask);
    this.asks = this.asks.filter((x) => !(x.kind === ask.kind && x.id === ask.id));
    this.changed();
  }

  /** Whether an ask is for someone whose key was checked here before, and changed since: a person
   * whose key was lost. */
  keyChanged(ask: Ask): boolean {
    return ask.kind === 'person' && ask.id in this.trusted;
  }

  /** Whether the person here waits for folder keys: a version of an encrypted folder they see
   * isn't sealed for them yet, so someone who has it seals it after a check. */
  get waitsForFolders(): boolean {
    return this.status === 'ready' && (this.answer?.folders ?? []).some((f) => !f.sealed);
  }

  /** How long until the next check-in: soon while a check runs, opened here with Show or
   * answered here, as the code and then the keys come right after the other side's turn; a little
   * later while this browser waits for keys; otherwise half a minute. */
  pace(): number {
    if (this.shown.size || this.answering.size) return 2_000;
    if (this.status === 'waiting' || this.waitsForFolders) return 5_000;
    return 30_000;
  }

  /** The newest version of a folder's key, signed by the root this browser trusts; null when the
   * folder has none. Throws SendRefused when the server shows fewer versions than this browser,
   * or the person's note, saw, or the newest isn't signed. */
  private async newestSigned(folder: string): Promise<FolderPublicKey | null> {
    const a = this.answer!;
    const versions = a.folders.filter((f) => f.folder === folder);
    const newest = versions.reduce<KeysAnswer['folders'][number] | null>((n, f) => (!n || f.version > n.version ? f : n), null);
    if ((newest?.version ?? 0) < (this.pins.folders[folder] ?? 0)) throw new SendRefused('the folder shows fewer versions of its key than seen before');
    if (!newest) return null;
    if (!(await this.signedKey(newest))) throw new SendRefused("the folder's newest key isn't signed by the root");
    return { folder, version: newest.version, publicKey: fromB64u(newest.public_key) };
  }

  /** What a new file into a folder is sealed for: its newest key, signed by the root this browser
   * trusts; null to send it plain, which only the root's plain statement for the folder's newest
   * version and its name allows, once there is a root. A folder switched off without one still
   * gets encrypted files, which the server takes. Throws SendRefused when nothing may go in. */
  async sendKey(folder: FolderInfo): Promise<FolderPublicKey | null> {
    // Over plain http nothing encrypts, and a page that came unprotected itself gains nothing
    // from checking the server's word: a plain folder takes files, an encrypted one none.
    if (!canEncrypt()) {
      if (folder.encrypted) throw new SendRefused("this page can't encrypt");
      return null;
    }
    if (!this.answer) await this.refresh();
    const a = this.answer;
    if (!a) throw new SendRefused("the keys couldn't be loaded");
    if (!this.root) {
      // No root anywhere yet: no folder can have keys, and everything goes plain.
      if (a.roots.length || this.pins.root || a.folders.some((f) => f.folder === folder.id)) throw new SendRefused('no root to check the keys with');
      return null;
    }
    const newest = await this.newestSigned(folder.id);
    if (newest && folder.encrypted) return newest;
    const plain = await this.signed(this.root, plainMessage(folder.id, newest?.version ?? 0, folder.name), folder.plain_signature);
    if (plain) return null;
    if (newest) return newest;
    throw new SendRefused('the folder is neither signed as plain nor has a signed key');
  }

  /** Whether changing folders needs the root's private key here, and it isn't: once there is a
   * root, only an admin's device that holds it makes folders, switches them and renames plain
   * ones. */
  get lacksRoot(): boolean {
    return !!this.answer?.roots.length && !this.rootKey;
  }

  /** A new version of a folder's key, signed by the root, sealed for this person and the recovery
   * key; it is open here from now on. */
  private async newFolderKey(folder: string, version: number): Promise<api.NewFolderKey> {
    if (!this.person || !this.rootKey) throw new NeedsRoot();
    const pair = await generateKeyPair(true);
    const aad = folderContext(folder, version);
    const sealed = await sealKey(this.person.publicKey, purposes.folder, aad, pair.raw!);
    const recoverySealed = await sealKey(this.rootKey.publicKey, purposes.folder, aad, pair.raw!);
    const signature = await sign(this.rootKey.raw!, this.rootKey.publicKey, folderKeyMessage(folder, version, pair.publicKey));
    this.folders.set(slot(folder, version), pair);
    return { public_key: b64u(pair.publicKey), signature: b64u(signature), sealed: b64u(sealed), recovery_sealed: b64u(recoverySealed) };
  }

  /** The root's plain statement for a folder. */
  private async plainSignature(folder: string, version: number, name: string): Promise<string> {
    if (!this.rootKey) throw new NeedsRoot();
    return b64u(await sign(this.rootKey.raw!, this.rootKey.publicKey, plainMessage(folder, version, name)));
  }

  /** Whether the server has a recovery key yet, which the first encrypted folder needs. */
  hasRecovery(): boolean {
    return !!this.answer?.roots.length;
  }

  /** Admins: turns encryption on for a folder, with its key's next version. */
  async encryptFolder(folder: FolderInfo): Promise<FolderInfo> {
    const key = await this.newFolderKey(folder.id, (folder.key_version ?? 0) + 1);
    const info = await api.setFolderEncryption(folder.id, { encrypted: true, key });
    await this.refresh();
    return info;
  }

  /** Admins: turns encryption off for a folder, with the root's plain statement. */
  async switchOff(folder: FolderInfo): Promise<FolderInfo> {
    const info = await api.setFolderEncryption(folder.id, { encrypted: false, plain_signature: await this.plainSignature(folder.id, folder.key_version ?? 0, folder.name) });
    await this.refresh();
    return info;
  }

  /** Admins: a new folder, signed as plain once there is a root, under an id picked here. */
  async createFolder(name: string): Promise<FolderInfo> {
    if (!this.hasRecovery()) return api.createFolder(name);
    const id = crypto.randomUUID();
    return api.createFolder(name, { id, plain_signature: await this.plainSignature(id, 0, name) });
  }

  /** Admins: a new name for a folder, signed anew for one that sends plain. */
  async renameFolder(folder: FolderInfo, name: string): Promise<FolderInfo> {
    if (folder.encrypted || !this.hasRecovery()) return api.renameFolder(folder.id, name);
    return api.renameFolder(folder.id, name, await this.plainSignature(folder.id, folder.key_version ?? 0, name));
  }

  /** Admins: a new recovery key; returns the code, which is shown once. The first signs every
   * folder as plain; a later one is signed by the one before, which this browser holds, and signs
   * anew all it signed, after checking each signature. */
  async makeRecovery(): Promise<string> {
    if (!this.person?.raw) throw new SealError("the person's key isn't open here");
    const r = await api.getRecovery();
    const old = r.roots.length ? this.rootKey : null;
    if (r.roots.length && (!old || r.roots.at(-1)!.public_key !== b64u(old.publicKey))) throw new NeedsRoot();
    const { secret, code } = newRecoveryCode();
    const pair = await generateKeyPair(true);
    const signWith = async (m: Bytes) => b64u(await sign(pair.raw!, pair.publicKey, m));
    const folderKeys: api.NewRecovery['folder_keys'] = [];
    for (const f of r.sign.folder_keys) {
      const m = folderKeyMessage(f.folder, f.version, fromB64u(f.public_key));
      if (!(await verify(old!.publicKey, m, fromB64u(f.signature)))) throw new SealError(`${f.folder}:${f.version} isn't signed by the recovery key`);
      folderKeys.push({ folder: f.folder, version: f.version, signature: await signWith(m) });
    }
    const plain: api.NewRecovery['plain'] = [];
    for (const f of r.sign.plain) {
      const m = plainMessage(f.folder, f.version, f.name);
      if (old && !(f.signature && (await verify(old.publicKey, m, fromB64u(f.signature))))) throw new SealError(`${f.name} isn't signed by the recovery key`);
      plain.push({ folder: f.folder, signature: await signWith(m) });
    }
    await api.putRecovery({
      public_key: b64u(pair.publicKey),
      locked: b64u(await lock(await secretKey(secret, purposes.recovery), recoveryContext, pair.raw!)),
      signature: old ? b64u(await sign(old.raw!, old.publicKey, rootMessage(pair.publicKey))) : null,
      sealed: b64u(await sealKey(this.person.publicKey, purposes.root, rootContext, pair.raw!)),
      folder_keys: folderKeys,
      plain,
    });
    await this.adoptRoot(pair.publicKey, []);
    this.rootKey = pair;
    await this.refresh();
    return code;
  }

  /** Admins: opens every encrypted folder with the recovery code and keeps its keys for this
   * person, and the recovery key's private key, which this browser trusts from now on; starts
   * over first when this browser has no person key. Throws SealError for a wrong code. */
  async useRecoveryCode(code: string): Promise<number> {
    const secret = parseRecoveryCode(code);
    if (!secret) throw new SealError('not a recovery code');
    const r = await api.getRecovery();
    if (!r.locked || !r.public_key) throw new SealError('no recovery key');
    const raw = await unlock(await secretKey(secret, purposes.recovery), recoveryContext, fromB64u(r.locked));
    const pub = fromB64u(r.public_key);
    if (!(await belongs(raw, pub))) throw new SealError('not the recovery key');
    const recovery = await keyPairOf(raw, pub);
    if (!this.person) await this.startOver();
    const person = this.person!;
    await this.adoptRoot(pub, r.roots);
    this.rootKey = recovery;
    const g = noGrants();
    for (const f of r.folders) {
      const aad = folderContext(f.folder, f.version);
      const fpub = fromB64u(f.public_key);
      if (!(await verify(pub, folderKeyMessage(f.folder, f.version, fpub), fromB64u(f.signature)))) continue;
      try {
        const folderRaw = await openKey(recovery, purposes.folder, aad, fromB64u(f.sealed));
        if (await belongs(folderRaw, fpub)) g.people.push({ folder: f.folder, version: f.version, user: this.me!.user.id, sealed: b64u(await sealKey(person.publicKey, purposes.folder, aad, folderRaw)) });
      } catch {
        // sealed for an older recovery key
      }
    }
    if (this.me!.user.role === 'admin') g.roots.push({ user: this.me!.user.id, sealed: b64u(await sealKey(person.publicKey, purposes.root, rootContext, raw)) });
    await api.postGrants(g);
    await this.refresh();
    return g.people.length;
  }

  /** Makes a new person key on this browser, when no other device of the person will come:
   * what was sealed for the old one goes, and the others seal again. */
  async startOver(): Promise<void> {
    const me = this.me!;
    this.device ??= await deviceKeyFor(me.device.id);
    const p = await generateKeyPair(true);
    const sealed = await sealKey(this.device.publicKey, purposes.person, personContext(me.user.id), p.raw!);
    await api.putPersonKey(b64u(p.publicKey), b64u(sealed), true);
    this.pins = { ...this.pins, person: b64u(p.publicKey) };
    await savePins(me.device.id, this.pins);
    this.person = p;
    await this.refresh();
  }

  /** The root this browser trusts, locked with a link's secret, for those who join with it. */
  private async lockedRoot(key: Bytes): Promise<string | undefined> {
    return this.root && !this.pins.tofu ? b64u(await lock(key, rootContext, this.root)) : undefined;
  }

  /** Every version of the keys of these folders (every encrypted one with null), and the root,
   * locked with a new secret for an invite's link. */
  async inviteKeys(folders: string[] | null): Promise<{ secret: string; keys: api.LockedFolderKey[]; root?: string }> {
    const secret = randomBytes(32);
    const key = await secretKey(secret, purposes.invite);
    const keys: api.LockedFolderKey[] = [];
    for (const [id, pair] of this.folders) {
      const [folder, version] = [id.slice(0, id.lastIndexOf(':')), Number(id.slice(id.lastIndexOf(':') + 1))];
      if (folders && !folders.includes(folder)) continue;
      keys.push({ folder, version, locked: b64u(await lock(key, folderContext(folder, version), pair.raw!)) });
    }
    return { secret: b64u(secret), keys, root: await this.lockedRoot(key) };
  }

  /** This person's own key and the root, locked with a new secret for the link of an invite for
   * another of their phones or browsers. */
  async personKeyForInvite(): Promise<{ secret: string; locked: string; root?: string } | null> {
    if (!this.person?.raw) return null;
    const secret = randomBytes(32);
    const key = await secretKey(secret, purposes.invite);
    const locked = await lock(key, personContext(this.me!.user.id), this.person.raw);
    return { secret: b64u(secret), locked: b64u(locked), root: await this.lockedRoot(key) };
  }

  /** What the link of a PIN that shows an encrypted folder needs: a new secret, locked with a key
   * from the folder's newest key, and the root and every version of the folder's key locked with
   * it. */
  async pinSecret(folder: string): Promise<{ secret: string; body: api.PinSecret } | null> {
    const newest = this.answer ? await this.newestSigned(folder).catch(() => null) : null;
    const holder = newest && this.folders.get(slot(folder, newest.version));
    if (!newest || !holder || !this.root) return null;
    const secret = randomBytes(32);
    const key = await secretKey(secret, purposes.pin);
    const locked = await lock(await pinSecretKey(holder.raw!), folderContext(folder, newest.version), secret);
    const keys: api.PinSecret['keys'] = [];
    for (const [id, pair] of this.folders) {
      if (!id.startsWith(folder + ':')) continue;
      const version = Number(id.slice(folder.length + 1));
      keys.push({ version, locked: b64u(await lock(key, folderContext(folder, version), pair.raw!)) });
    }
    return { secret: b64u(secret), body: { locked: b64u(locked), version: newest.version, root: b64u(await lock(key, rootContext, this.root)), keys } };
  }

  /** The secret of the link of a PIN that shows an encrypted folder, opened with a key from the
   * folder's key; null when there is none, or that key isn't open here. */
  async pinLinkSecret(pin: { folder: string; secret: { locked: string; version: number } | null }): Promise<string | null> {
    const pair = pin.secret && this.folders.get(slot(pin.folder, pin.secret.version));
    if (!pin.secret || !pair) return null;
    try {
      return b64u(await unlock(await pinSecretKey(pair.raw!), folderContext(pin.folder, pin.secret.version), fromB64u(pin.secret.locked)));
    } catch {
      return null;
    }
  }

  /** What follows the code in the link of a PIN that only sends into a folder with keys: the
   * root's fingerprint, which guests' browsers check the folder's key with; null otherwise. */
  async pinLinkRoot(folder: string): Promise<string | null> {
    if (!this.root || this.pins.tofu || !this.answer?.folders.some((f) => f.folder === folder)) return null;
    return b64u(await fingerprint(this.root));
  }

  /** Admins moving encrypted files: their keys, sealed for the target folder's newest key, which
   * the root must have signed. */
  async moveKeys(files: { id: string; folder: string; enc: FileEnc | null }[], target: string): Promise<api.MovedKey[]> {
    const out: api.MovedKey[] = [];
    let newest: FolderPublicKey | null | undefined;
    for (const f of files) {
      if (!f.enc || f.folder === target) continue;
      newest ??= await this.newestSigned(target);
      if (!newest) throw new SealError('the folder was never encrypted');
      const key = await this.fileKey({ id: f.id, folder: f.folder, enc: f.enc });
      const sealed = await sealKey(newest.publicKey, purposes.file, folderContext(target, newest.version), key);
      out.push({ id: f.id, version: newest.version, key: b64u(sealed) });
    }
    return out;
  }

  /** A PIN guest's keys: the folder's keys and the root, locked with the secret of the PIN's
   * link. */
  async openPinKeys(secret: string): Promise<number> {
    const k = await api.getPinKeys();
    const key = await secretKey(fromB64u(secret), purposes.pin);
    if (k.root) this.guestRoot = await unlock(key, rootContext, fromB64u(k.root)).catch(() => null);
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

/** A grant with nothing in it yet. */
function noGrants(): api.Grants {
  return { devices: [], people: [], recovery: [], pins: [], roots: [] };
}

/** The keys of this page. */
export const keyring = new Keyring();

/** Sets up the keys of a browser that just accepted an invite, with the keys its link's secret
 * unlocks: the person's own (a new phone or browser of someone), or folder keys for a new
 * person, which get sealed for their new person key; and the root to trust. Without a secret, or
 * with a wrong one, the browser waits for the others like any new one. */
export async function keysFromInvite(me: Me, secret: string | null, keys: api.InviteKey[], lockedRoot: string | null): Promise<void> {
  const device = await deviceKeyFor(me.device.id);
  await api.putDeviceKey(b64u(device.publicKey));
  if (!secret) return;
  const key = await secretKey(fromB64u(secret), purposes.invite);
  const root = lockedRoot ? await unlock(key, rootContext, fromB64u(lockedRoot)).catch(() => null) : null;
  const pins: Pins = { ...(device.pins ?? { folders: {} }), ...(root ? { root: b64u(root), tofu: undefined } : {}) };
  const own = keys.find((k) => k.folder === null);
  if (own) {
    const raw = await unlock(key, personContext(me.user.id), fromB64u(own.locked));
    const pub = fromB64u(own.public_key);
    if (!(await belongs(raw, pub))) return;
    await savePins(me.device.id, { ...pins, person: own.public_key });
    const sealed = await sealKey(device.publicKey, purposes.person, personContext(me.user.id), raw);
    await api.postGrants({ ...noGrants(), devices: [{ device: me.device.id, sealed: b64u(sealed) }] });
    return;
  }
  const person = await generateKeyPair(true);
  const sealed = await sealKey(device.publicKey, purposes.person, personContext(me.user.id), person.raw!);
  try {
    await api.putPersonKey(b64u(person.publicKey), b64u(sealed));
  } catch (e) {
    if (e instanceof ApiError && e.code === 'key_exists') return; // someone joining twice: the key there stays
    throw e;
  }
  await savePins(me.device.id, { ...pins, person: b64u(person.publicKey) });
  const g = noGrants();
  for (const k of keys) {
    if (!k.folder) continue;
    const aad = folderContext(k.folder, k.version);
    const pub = fromB64u(k.public_key);
    // Only keys the root the link brought signed.
    if (!root || !k.signature || !(await verify(root, folderKeyMessage(k.folder, k.version, pub), fromB64u(k.signature)))) continue;
    try {
      const raw = await unlock(key, aad, fromB64u(k.locked));
      if (await belongs(raw, pub)) g.people.push({ folder: k.folder, version: k.version, user: me.user.id, sealed: b64u(await sealKey(person.publicKey, purposes.folder, aad, raw)) });
    } catch {
      // locked with another secret
    }
  }
  if (g.people.length) await api.postGrants(g);
}
