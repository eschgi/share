// Folders, as everyone sees them: the library shows one of them or all, sending goes into one.
// Only with a second folder is there anything to choose, and only then do the pages talk of
// folders. Pure decisions, kept here so the tests can check them.
import type { FolderInfo, People, Role } from '../../api';

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

/** Someone who sees a folder: an admin, a member given it, or an open invite (pending). */
export interface Seer {
  id: string;
  name: string;
  role: Role;
  pending: boolean;
}

/** Who sees a folder: the admins, then the members given it, then the open invites for new
 * people that give it (an invite for a new admin gives every folder). */
export function whoSees(people: People, folder: string): Seer[] {
  const seer = (id: string, name: string, role: Role, pending: boolean): Seer => ({ id, name, role, pending });
  return [
    ...people.users.filter((u) => u.role === 'admin').map((u) => seer(u.id, u.name, u.role, false)),
    ...people.users.filter((u) => u.role !== 'admin' && u.folders.includes(folder)).map((u) => seer(u.id, u.name, u.role, false)),
    ...people.invites.filter((i) => i.user_id === null && (i.role === 'admin' || i.folders.includes(folder))).map((i) => seer(i.id, i.name, i.role, true)),
  ];
}

/** The folders an invite for a new member gives at first: the one the library shows, else the
 * oldest. */
export function inviteDefault(list: readonly FolderInfo[], shown: string | null): string[] {
  const first = (shown !== null && list.some((f) => f.id === shown) ? shown : list[0]?.id) ?? null;
  return first === null ? [] : [first];
}

/** The folder files move into at first: the first one they aren't in already. */
export function moveDefault(list: readonly FolderInfo[], here: string | null): string | null {
  return list.find((f) => f.id !== here)?.id ?? null;
}
