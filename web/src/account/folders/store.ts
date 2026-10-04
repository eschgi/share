// The folders this person sees, for all the account's pages: the library shows one of them or
// all, and the choice stays in this browser. They are fetched when the pages open, when the
// library changes and after an admin changes them.
import { useEffect, useState } from 'preact/hooks';
import { getFolders, type FolderInfo } from '../../api';
import { sendTarget, validShown } from './model';

const storedKey = 'share.folder';

let list: FolderInfo[] | null = null;
let failed = false;
let loading: Promise<void> | null = null;
let stored: string | null = readStored();
/** The folder chosen on the Send tab, for this visit. */
let sendChoice: string | null = null;
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

/** Sending goes into this folder from now on, for this visit. */
export function chooseSendFolder(id: string): void {
  sendChoice = id;
  changed();
}

/** The folders as they are now, and the folder sending goes into; for code outside the pages. */
export function foldersNow(): { list: FolderInfo[] | null; shown: string | null; sendTo: string | null } {
  const shown = validShown(list, stored);
  return { list, shown, sendTo: list ? sendTarget(list, shown, sendChoice) : null };
}

/** A folder the library showed is gone, or the person doesn't see it any more: all folders. */
export function folderGone(id: string): void {
  if (sendChoice === id) sendChoice = null;
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
  /** The folder sending goes into: chosen on the Send tab, or the one the library shows. */
  sendTo: FolderInfo | null;
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
  const now = foldersNow();
  return {
    list,
    failed,
    shown: list?.find((f) => f.id === now.shown) ?? null,
    sendTo: list?.find((f) => f.id === now.sendTo) ?? null,
    byId: (id) => list?.find((f) => f.id === id),
  };
}
