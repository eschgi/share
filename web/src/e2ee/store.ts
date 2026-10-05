// This browser's device keys, in IndexedDB: the private key as a CryptoKey that can't be
// exported, by the id of the device the server knows this browser as.
import type { Bytes } from './bytes';

export interface DeviceKey {
  deviceId: string;
  privateKey: CryptoKey;
  publicKey: Bytes;
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

/** Forgets the keys of other devices this browser was before, e.g. after signing out. */
export async function keepOnly(deviceId: string): Promise<void> {
  const keys = await run<IDBValidKey[]>('readonly', (s) => s.getAllKeys());
  for (const k of keys) if (k !== deviceId) await run('readwrite', (s) => s.delete(k));
}
