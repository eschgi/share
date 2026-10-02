// Which files are in the save folder, for the marks on the tiles, as the app marks files on the
// phone. Saving into a folder marks each file it writes or finds there; a single download or a
// ZIP can't be followed, so they don't count. Whenever the browser lets the page look into the
// folder without asking, the marks are checked against it once a visit.
import { useEffect, useState } from 'preact/hooks';
import { folderOf, keptDb, keptFolder } from './folder';
import type { Folder } from './engine';

export interface Mark {
  /** Where in the folder: "2026-09-27/IMG_0001.jpg". */
  path: string;
  size: number;
}

let marks: Map<string, Mark> | null = null;
let loading: Promise<void> | null = null;
let folderName = '';
let checked = false;
/** Marks not written down yet: saving finds many files at once. */
const pending = new Map<string, Mark>();
let flushing = 0;
const listeners = new Set<() => void>();
const changed = () => listeners.forEach((l) => l());

async function load(): Promise<void> {
  const [dir, all] = await Promise.all([keptFolder(), readAll()]);
  folderName = dir?.name ?? '';
  marks = new Map([...all, ...(marks ?? [])]);
  changed();
  if (dir && !checked) {
    checked = true;
    await check(dir).catch(() => {});
  }
}

async function readAll(): Promise<Map<string, Mark>> {
  try {
    const d = await keptDb();
    const out = await new Promise<Map<string, Mark>>((resolve, reject) => {
      const out = new Map<string, Mark>();
      const req = d.transaction('saved').objectStore('saved').openCursor();
      req.onsuccess = () => {
        const c = req.result;
        if (!c) return resolve(out);
        out.set(String(c.key), c.value as Mark);
        c.continue();
      };
      req.onerror = () => reject(req.error);
    });
    d.close();
    return out;
  } catch {
    return new Map();
  }
}

async function write(change: (store: IDBObjectStore) => void): Promise<void> {
  try {
    const d = await keptDb();
    await new Promise<void>((resolve, reject) => {
      const tx = d.transaction('saved', 'readwrite');
      change(tx.objectStore('saved'));
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
    d.close();
  } catch {
    // Not kept, then: the marks show until the page closes.
  }
}

/** The files whose mark is wrong: missing from the folder, or another size there. */
export async function wrongMarks(all: Map<string, Mark>, folder: Folder): Promise<string[]> {
  const wrong: string[] = [];
  for (const [id, m] of all) {
    const size = await folder.sizeOf(m.path).catch(() => null);
    if (size !== m.size) wrong.push(id);
  }
  return wrong;
}

/** Drops the marks of files no longer in the folder, if the page may look without asking. */
async function check(dir: FileSystemDirectoryHandle): Promise<void> {
  const d = dir as FileSystemDirectoryHandle & { queryPermission(o: { mode: 'read' }): Promise<string> };
  if (!marks?.size || (await d.queryPermission({ mode: 'read' })) !== 'granted') return;
  const wrong = await wrongMarks(new Map(marks), folderOf(dir));
  if (!wrong.length) return;
  for (const id of wrong) marks.delete(id);
  changed();
  await write((s) => wrong.forEach((id) => s.delete(id)));
}

/** Marks a file as saved into the folder at path. */
export function markSaved(id: string, mark: Mark, folder: string): void {
  marks ??= new Map();
  marks.set(id, mark);
  folderName = folder;
  pending.set(id, mark);
  flushing ||= window.setTimeout(() => {
    flushing = 0;
    const batch = [...pending];
    pending.clear();
    void write((s) => batch.forEach(([id, m]) => s.put(m, id)));
  }, 300);
  changed();
}

/** Another folder was picked: none of the files are in it. Its database store is emptied apart. */
export function forgetMarks(): void {
  marks = new Map();
  pending.clear();
  changed();
}

/** Signing out: the next person in this browser doesn't see what was saved. */
export async function clearMarks(): Promise<void> {
  forgetMarks();
  await write((s) => s.clear());
}

export interface SavedMarks {
  /** Whether the file is in the save folder. */
  has: (id: string) => boolean;
  /** The folder's name. */
  folder: string;
}

export function useSavedMarks(): SavedMarks {
  const [, redraw] = useState(0);
  useEffect(() => {
    const l = () => redraw((n) => n + 1);
    listeners.add(l);
    loading ??= load();
    return () => void listeners.delete(l);
  }, []);
  return { has: (id) => marks?.has(id) ?? false, folder: folderName };
}
