// Folders, as everyone sees them: the library shows one of them or all, sending goes into one.
// Only with a second folder is there anything to choose, and only then do the pages talk of
// folders. Pure decisions, kept here so the tests can check them.
import type { FolderInfo } from '../../api';

/** There is a folder to choose: the person sees more than one. */
export function hasChoices(list: readonly FolderInfo[] | null): boolean {
  return (list?.length ?? 0) > 1;
}

/** The folder the library shows: the one stored, while the person still sees it; null for all. */
export function validShown(list: readonly FolderInfo[] | null, stored: string | null): string | null {
  return stored !== null && list?.some((f) => f.id === stored) ? stored : null;
}

/** What all the folders hold together. */
export function allTotals(list: readonly FolderInfo[]): { files: number; bytes: number } {
  return list.reduce((t, f) => ({ files: t.files + f.files, bytes: t.bytes + f.bytes }), { files: 0, bytes: 0 });
}

/** The folder sending goes into: the one chosen for it, else the one the library shows, else
 * the oldest. null while the person sees no folder. */
export function sendTarget(list: readonly FolderInfo[], shown: string | null, chosen: string | null): string | null {
  for (const id of [chosen, shown]) if (id !== null && list.some((f) => f.id === id)) return id;
  return list[0]?.id ?? null;
}

/** Where files dropped on a page go: with one folder into it; in the library into the folder
 * shown, on the Send tab into the folder chosen there. Elsewhere, and while the library shows
 * all folders, the person is asked first. */
export function dropTarget(path: string, list: readonly FolderInfo[], shown: string | null, sendTo: string | null): string | 'ask' {
  if (list.length === 1) return list[0].id;
  if (list.length === 0) return 'ask';
  if (path.startsWith('/library') && shown !== null) return shown;
  if (path.startsWith('/send') && sendTo !== null) return sendTo;
  return 'ask';
}
