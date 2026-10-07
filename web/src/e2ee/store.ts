// This browser's device keys, in IndexedDB: the private key as a CryptoKey that can't be
// exported, by the id of the device the server knows this browser as, with what this browser
// keeps of the keys it was given (docs/e2ee-plan.md).
import type { Bytes } from './bytes';

export interface DeviceKey {
  deviceId: string;
  privateKey: CryptoKey;
  publicKey: Bytes;
  /** The people's keys this browser checked before passing folder keys on: user id to key. */
  trusted?: Record<string, string>;
  /** What this browser keeps of what the server says about keys, so that a changed database
   * can't take it back. */
  pins?: Pins;
}

/** The person's public key this browser holds the private key of; the root it trusts, and
 * whether it only took it from the server, the first time (tofu), or from where a database can't
 * change it; and the newest version of each folder's key it saw, signed by the root. */
export interface Pins {
  person?: string;
  root?: string;
  tofu?: boolean;
  folders: Record<string, number>;
}

const dbName = 'share-keys';

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(dbName, 1);
    req.onupgradeneeded = () => req.result.createObjectStore('devices', { keyPath: 'deviceId' });
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

function run<T>(mode: IDBTransactionMode, f: (s: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return open().then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        const tx = db.transaction('devices', mode);
        const req = f(tx.objectStore('devices'));
        tx.oncomplete = () => {
          db.close();
          resolve(req.result);
        };
        tx.onerror = tx.onabort = () => {
          db.close();
          reject(tx.error);
        };
      }),
  );
}

export const loadDeviceKey = (deviceId: string) => run<DeviceKey | undefined>('readonly', (s) => s.get(deviceId));

export const saveDeviceKey = (k: DeviceKey) => run('readwrite', (s) => s.put(k));

/** The people's keys this browser checked, kept with its device key. */
export const loadTrusted = async (deviceId: string) => (await loadDeviceKey(deviceId))?.trusted ?? {};

export async function saveTrusted(deviceId: string, trusted: Record<string, string>): Promise<void> {
  const k = await loadDeviceKey(deviceId);
  if (k) await saveDeviceKey({ ...k, trusted });
}

/** What this browser keeps of the keys, kept with its device key. */
export async function savePins(deviceId: string, pins: Pins): Promise<void> {
  const k = await loadDeviceKey(deviceId);
  if (k) await saveDeviceKey({ ...k, pins });
}

/** Forgets the keys of other devices this browser was before, e.g. after signing out. */
export async function keepOnly(deviceId: string): Promise<void> {
  const keys = await run<IDBValidKey[]>('readonly', (s) => s.getAllKeys());
  for (const k of keys) if (k !== deviceId) await run('readwrite', (s) => s.delete(k));
}
