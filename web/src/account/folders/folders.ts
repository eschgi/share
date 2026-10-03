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
