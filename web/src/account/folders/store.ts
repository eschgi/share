// The folders this person sees, for all the account's pages: the library shows one of them or
// all, and the choice stays in this browser. They are fetched when the pages open, when the
// library changes and after an admin changes them.
import { useEffect, useState } from 'preact/hooks';
import { getFolders, type FolderInfo } from '../../api';
import { validShown } from './folders';

const storedKey = 'share.folder';

let list: FolderInfo[] | null = null;
let failed = false;
let loading: Promise<void> | null = null;
let stored: string | null = readStored();
const listeners = new Set<() => void>();
const changed = () => listeners.forEach((l) => l());

function readStored(): string | null {
  try {
    return localStorage.getItem(storedKey);
  } catch {
    return null;
  }
}

function writeStored(id: string | null): void {
  try {
    if (id === null) localStorage.removeItem(storedKey);
    else localStorage.setItem(storedKey, id);
  } catch {
    // private windows may refuse: the choice lasts for this visit
  }
}

/** Fetches the folders again; a fetch already under way is reused. */
export function refreshFolders(): Promise<void> {
  loading ??= getFolders()
    .then((r) => {
      list = r.folders;
      failed = false;
    })
    .catch(() => {
      failed = true;
    })
    .finally(() => {
      loading = null;
      changed();
    });
  return loading;
}

/** The library shows this folder from now on; null shows all. */
export function showFolder(id: string | null): void {
  stored = id;
  writeStored(id);
  changed();
}

/** A folder the library showed is gone, or the person doesn't see it any more: all folders. */
export function folderGone(id: string): void {
  if (stored === id) showFolder(null);
  void refreshFolders();
}

/** Signed out: the next person starts with all folders. */
export function forgetFolders(): void {
  list = null;
  showFolder(null);
}

export interface Folders {
  /** null until they are fetched. */
  list: FolderInfo[] | null;
  failed: boolean;
  /** The folder the library shows; null for all. */
  shown: FolderInfo | null;
  byId: (id: string) => FolderInfo | undefined;
}

/** The folders, kept up to date. */
export function useFolders(): Folders {
  const [, redraw] = useState(0);
  useEffect(() => {
    const l = () => redraw((n) => n + 1);
    listeners.add(l);
    if (list === null) void refreshFolders();
    return () => void listeners.delete(l);
  }, []);
  const shownId = validShown(list, stored);
  return {
    list,
    failed,
    shown: list?.find((f) => f.id === shownId) ?? null,
    byId: (id) => list?.find((f) => f.id === id),
  };
}
