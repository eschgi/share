// Files shared into the installed website from other apps, through Android's share sheet. The
// service worker keeps them here, in IndexedDB, until a page has sent them (incoming.ts). Both
// use this file, so it needs nothing but IndexedDB.

export interface Shared {
  key: number;
  /** One share: its files are sent by one page together. */
  batch: string;
  file: File;
  /** When it was shared, in ms. */
  at: number;
}

/** Shared files not sent within a week go, as unfinished uploads do on the server. */
export const keepShared = 7 * 24 * 60 * 60 * 1000;

/** The inbox's files, split into those still to send and those too old to. */
export function sortShared(all: Shared[], now: number): { fresh: Shared[]; old: Shared[] } {
  const fresh = all.filter((s) => now - s.at <= keepShared);
  return { fresh, old: all.filter((s) => !fresh.includes(s)) };
}

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open('share-inbox', 1);
    req.onupgradeneeded = () => req.result.createObjectStore('files', { keyPath: 'key', autoIncrement: true });
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

/** Runs work in one transaction, all or nothing. */
async function inTransaction<T>(mode: IDBTransactionMode, work: (files: IDBObjectStore) => IDBRequest<T> | void): Promise<T | undefined> {
  const d = await open();
  try {
    return await new Promise<T | undefined>((resolve, reject) => {
      const tx = d.transaction('files', mode);
      const req = work(tx.objectStore('files'));
      tx.oncomplete = () => resolve(req ? req.result : undefined);
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  } finally {
    d.close();
  }
}

/** Keeps the files of one share. */
export async function stash(files: File[], batch: string, at: number): Promise<void> {
  await inTransaction('readwrite', (s) => {
    for (const file of files) s.add({ batch, file, at });
  });
}

export async function inbox(): Promise<Shared[]> {
  return ((await inTransaction('readonly', (s) => s.getAll())) ?? []) as Shared[];
}

export async function removeShared(keys: number[]): Promise<void> {
  if (keys.length === 0) return;
  await inTransaction('readwrite', (s) => keys.forEach((k) => s.delete(k)));
}

export async function clearShared(): Promise<void> {
  await inTransaction('readwrite', (s) => s.clear());
}
