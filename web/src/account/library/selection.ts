// What is selected in the library. Each upload day is kept as "just these files" or "all but
// these", so a day's circle and Select all need nothing loaded and still give exact counts and
// sizes, from the days' totals. The ids of whole days come from the server only when the files
// are downloaded or deleted. Values never change; every step makes a new one.
import type { FileInfo, LibraryDay } from '../../api';

interface DayPick {
  /** false: just ids; true: every file of the day but ids. */
  all: boolean;
  ids: ReadonlySet<string>;
}

export interface Selection {
  readonly days: ReadonlyMap<string, DayPick>;
  /** The sizes of the files picked or left out one by one. */
  readonly sizes: ReadonlyMap<string, number>;
}

export const noSelection: Selection = { days: new Map(), sizes: new Map() };

export type DayState = 'none' | 'some' | 'all';

export function isEmpty(s: Selection): boolean {
  return s.days.size === 0;
}

export function isSelected(s: Selection, f: FileInfo): boolean {
  const pick = s.days.get(f.day);
  if (!pick) return false;
  return pick.all !== pick.ids.has(f.id);
}

/** How many of a day's files are selected. */
export function dayCount(s: Selection, day: LibraryDay): number {
  const pick = s.days.get(day.day);
  if (!pick) return 0;
  return pick.all ? Math.max(0, day.count - pick.ids.size) : pick.ids.size;
}

function dayBytes(s: Selection, day: LibraryDay): number {
  const pick = s.days.get(day.day);
  if (!pick) return 0;
  let named = 0;
  for (const id of pick.ids) named += s.sizes.get(id) ?? 0;
  return pick.all ? Math.max(0, day.bytes - named) : named;
}

export function dayState(s: Selection, day: LibraryDay): DayState {
  const n = dayCount(s, day);
  return n === 0 ? 'none' : n >= day.count ? 'all' : 'some';
}

/** How many files are selected, and how big they are together. */
export function totals(s: Selection, days: readonly LibraryDay[]): { count: number; bytes: number } {
  let count = 0;
  let bytes = 0;
  for (const day of days) {
    count += dayCount(s, day);
    bytes += dayBytes(s, day);
  }
  return { count, bytes };
}

/** Selects or lets go of some files; each day's total says when a day is complete. */
export function setFiles(s: Selection, files: readonly FileInfo[], on: boolean, days: readonly LibraryDay[]): Selection {
  const counts = new Map(days.map((d) => [d.day, d.count]));
  const next = new Map(s.days);
  const sizes = new Map(s.sizes);
  for (const f of files) {
    const pick = next.get(f.day) ?? { all: false, ids: new Set<string>() };
    const selected = pick.all !== pick.ids.has(f.id);
    if (selected === on) continue;
    const ids = new Set(pick.ids);
    // Picking a file adds it to "just these" or takes it off "all but these", and the other way.
    if (on !== pick.all) ids.add(f.id);
    else ids.delete(f.id);
    sizes.set(f.id, f.size);
    const count = counts.get(f.day) ?? Infinity;
    if (!pick.all && ids.size >= count) next.set(f.day, { all: true, ids: new Set() });
    else if ((pick.all && ids.size >= count) || (!pick.all && ids.size === 0)) next.delete(f.day);
    else next.set(f.day, { all: pick.all, ids });
  }
  return { days: next, sizes };
}

/** A day's circle: all of the day, or none of it. */
export function setDay(s: Selection, day: string, on: boolean): Selection {
  const next = new Map(s.days);
  if (on) next.set(day, { all: true, ids: new Set() });
  else next.delete(day);
  return { days: next, sizes: s.sizes };
}

/** Select all: every day of the library as it is filtered now. */
export function selectAll(days: readonly LibraryDay[]): Selection {
  return { days: new Map(days.map((d) => [d.day, { all: true, ids: new Set<string>() }])), sizes: new Map() };
}

/** The selected files that are loaded, in the list's order. */
export function loadedSelected(s: Selection, files: readonly FileInfo[]): FileInfo[] {
  return files.filter((f) => isSelected(s, f));
}

/** The folder the selected files are all in; null when they are in several, or when some aren't
 * loaded (a day picked as a whole), so the page can't tell. */
export function commonFolder(s: Selection, files: readonly FileInfo[], days: readonly LibraryDay[]): string | null {
  const known = loadedSelected(s, files);
  if (known.length === 0 || known.length < totals(s, days).count) return null;
  const first = known[0].folder;
  return known.every((f) => f.folder === first) ? first : null;
}

/** Every selected id, asking for the ids of the days picked as a whole (the list's filter). */
export async function selectedIds(s: Selection, idsOfDay: (day: string) => Promise<string[]>): Promise<string[]> {
  const out: string[] = [];
  for (const [day, pick] of s.days) {
    if (!pick.all) out.push(...pick.ids);
    else for (const id of await idsOfDay(day)) if (!pick.ids.has(id)) out.push(id);
  }
  return out;
}
