// Chrome's and Edge's way into a folder on the computer (the File System Access API): picking
// it, keeping it for next time, the right to write there, and the engine's folder and network
// on top of them. Other browsers, and plain http at home, download a ZIP instead.
import { contentPath } from '../../api';
import type { Answer, FetchFile, Folder } from './engine';
import { forgetMarks } from './marks';

type Permission = 'granted' | 'denied' | 'prompt';
type WithPermission = FileSystemDirectoryHandle & {
  queryPermission(d: { mode: 'readwrite' }): Promise<Permission>;
  requestPermission(d: { mode: 'readwrite' }): Promise<Permission>;
};
type Picker = (o: { id: string; mode: 'readwrite'; startIn: 'downloads' }) => Promise<FileSystemDirectoryHandle>;

const picker = () => (window as Window & { showDirectoryPicker?: Picker }).showDirectoryPicker;

/** Whether this browser can save into a folder. */
export function canSaveToFolder(): boolean {
  return isSecureContext && typeof picker() === 'function';
}

/** Lets the person pick a folder, which is kept for next time; null if they don't. In a click. */
export async function pickFolder(): Promise<FileSystemDirectoryHandle | null> {
  try {
    const dir = await picker()!({ id: 'share-save', mode: 'readwrite', startIn: 'downloads' });
    await keep(dir).catch(() => {});
    return dir;
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') return null;
    throw e;
  }
}

/** Whether the page may write into the folder; in a click, the browser asks if it has to. */
export async function mayWrite(dir: FileSystemDirectoryHandle): Promise<boolean> {
  const d = dir as WithPermission;
  try {
    if ((await d.queryPermission({ mode: 'readwrite' })) === 'granted') return true;
    return (await d.requestPermission({ mode: 'readwrite' })) === 'granted';
  } catch {
    return false; // the folder is gone, or the browser forgot it
  }
}

// The folder picked last time, kept in IndexedDB: a folder can't go into localStorage. The
// same database keeps which files are in it (marks.ts).

/** share-kept: the folder (kept) and the files saved into it (saved), by file id. */
export function keptDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open('share-kept', 2);
    req.onupgradeneeded = () => {
      const d = req.result;
      if (!d.objectStoreNames.contains('kept')) d.createObjectStore('kept');
      if (!d.objectStoreNames.contains('saved')) d.createObjectStore('saved');
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

/** Keeps the folder for next time. Another folder than before has none of the saved files. */
async function keep(dir: FileSystemDirectoryHandle): Promise<void> {
  const before = await keptFolder();
  const same = before !== null && (await before.isSameEntry(dir).catch(() => false));
  const d = await keptDb();
  await new Promise<void>((resolve, reject) => {
    const tx = d.transaction(['kept', 'saved'], 'readwrite');
    tx.objectStore('kept').put(dir, 'saveFolder');
    if (!same) tx.objectStore('saved').clear();
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
  d.close();
  if (!same) forgetMarks();
}

/** The folder picked last time, if any. */
export async function keptFolder(): Promise<FileSystemDirectoryHandle | null> {
  try {
    const d = await keptDb();
    const dir = await new Promise<FileSystemDirectoryHandle | undefined>((resolve, reject) => {
      const req = d.transaction('kept').objectStore('kept').get('saveFolder');
      req.onsuccess = () => resolve(req.result as FileSystemDirectoryHandle | undefined);
      req.onerror = () => reject(req.error);
    });
    d.close();
    return dir ?? null;
  } catch {
    return null;
  }
}

/** The engine's folder: paths such as "2026-09-27/IMG_0001.jpg" inside the picked one. */
export function folderOf(root: FileSystemDirectoryHandle): Folder {
  const made = new Map<string, Promise<FileSystemDirectoryHandle>>();
  const walk = async (parts: string[], create: boolean) => {
    let dir = root;
    for (const part of parts) dir = await dir.getDirectoryHandle(part, { create });
    return dir;
  };
  const split = (path: string) => {
    const parts = path.split('/');
    const name = parts.pop()!;
    return { parts, name };
  };
  return {
    async sizeOf(path) {
      const { parts, name } = split(path);
      try {
        const dir = await walk(parts, false);
        return (await (await dir.getFileHandle(name)).getFile()).size;
      } catch (e) {
        if (e instanceof DOMException && e.name === 'NotFoundError') return null;
        if (e instanceof DOMException && e.name === 'TypeMismatchError') return -1; // a folder has that name
        throw e;
      }
    },
    async create(path) {
      const { parts, name } = split(path);
      const key = parts.join('/');
      let dir = made.get(key);
      if (!dir) {
        dir = walk(parts, true);
        made.set(key, dir);
        dir.catch(() => made.delete(key));
      }
      const d = await dir;
      const out = await (await d.getFileHandle(name, { create: true })).createWritable();
      return {
        write: (chunk) => out.write(chunk as Uint8Array<ArrayBuffer>),
        close: () => out.close(),
        // Nothing half written stays: the browser drops what was written, and the empty file goes.
        abort: async () => {
          await out.abort().catch(() => {});
          await d.removeEntry(name).catch(() => {});
        },
      };
    },
  };
}

async function* chunksOf(body: ReadableStream<Uint8Array>): AsyncIterable<Uint8Array> {
  const reader = body.getReader();
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) return;
      yield value;
    }
  } finally {
    reader.cancel().catch(() => {});
  }
}

async function* nothing(): AsyncIterable<Uint8Array> {}

/** The engine's network: a file from a byte on, the rest only if it is still the same file. */
export const fetchFile: FetchFile = async (id, from, etag, signal): Promise<Answer> => {
  const headers = new Headers();
  if (from > 0) {
    headers.set('Range', `bytes=${from}-`);
    if (etag) headers.set('If-Range', etag);
  }
  const res = await fetch(contentPath(id), { headers, signal, cache: 'no-store' });
  // With the files in a bucket the answer comes from there, after the server's redirect.
  const remote = res.url !== '' && new URL(res.url).origin !== location.origin;
  return { status: res.status, etag: res.headers.get('ETag'), body: res.ok && res.body ? chunksOf(res.body) : nothing(), remote };
};
