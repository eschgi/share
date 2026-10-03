import { describe, expect, it } from 'vitest';
import type { FileInfo, LibraryDay } from '../src/api';
import {
  commonFolder,
  dayCount,
  dayState,
  isEmpty,
  isSelected,
  loadedSelected,
  noSelection,
  selectAll,
  selectedIds,
  setDay,
  setFiles,
  totals,
} from '../src/account/library/selection';

const f = (id: string, day: string, size = 10) => ({ id, day, size }) as FileInfo;
// Today has 3 files, of which 2 are loaded; yesterday has 2.
const days: LibraryDay[] = [
  { day: 'today', count: 3, bytes: 300 },
  { day: 'yesterday', count: 2, bytes: 25 },
];
const a = f('a', 'today', 100);
const b = f('b', 'today', 120);
const y1 = f('y1', 'yesterday', 10);
const y2 = f('y2', 'yesterday', 15);

describe('selection', () => {
  it('picks single files, and counts them', () => {
    let s = setFiles(noSelection, [a], true, days);
    expect(isSelected(s, a)).toBe(true);
    expect(isSelected(s, b)).toBe(false);
    expect(dayState(s, days[0])).toBe('some');
    expect(totals(s, days)).toEqual({ count: 1, bytes: 100 });
    s = setFiles(s, [a], false, days);
    expect(isEmpty(s)).toBe(true);
  });

  it('takes a whole day by its circle, without its files loaded', () => {
    const s = setDay(noSelection, 'today', true);
    expect(dayState(s, days[0])).toBe('all');
    expect(isSelected(s, a)).toBe(true);
    expect(totals(s, days)).toEqual({ count: 3, bytes: 300 });
    expect(isEmpty(setDay(s, 'today', false))).toBe(true);
  });

  it('leaves single files out of a whole day, with exact sizes', () => {
    let s = setDay(noSelection, 'today', true);
    s = setFiles(s, [b], false, days);
    expect(isSelected(s, b)).toBe(false);
    expect(dayState(s, days[0])).toBe('some');
    expect(totals(s, days)).toEqual({ count: 2, bytes: 180 });
    // Picking it again makes the day whole.
    s = setFiles(s, [b], true, days);
    expect(dayState(s, days[0])).toBe('all');
    expect(totals(s, days)).toEqual({ count: 3, bytes: 300 });
  });

  it('turns a day picked file by file into a whole one, and back into nothing', () => {
    let s = setFiles(noSelection, [y1, y2], true, days);
    expect(dayState(s, days[1])).toBe('all');
    s = setFiles(s, [y1, y2], false, days);
    expect(dayState(s, days[1])).toBe('none');
    expect(isEmpty(s)).toBe(true);
  });

  it('selects everything, also what is not loaded', () => {
    const s = selectAll(days);
    expect(totals(s, days)).toEqual({ count: 5, bytes: 325 });
    expect(loadedSelected(s, [a, b, y1, y2]).map((x) => x.id)).toEqual(['a', 'b', 'y1', 'y2']);
  });

  it('asks for the ids of whole days only, and leaves out what was taken off', async () => {
    let s = setDay(noSelection, 'today', true);
    s = setFiles(s, [b], false, days);
    s = setFiles(s, [y2], true, days);
    const asked: string[] = [];
    const ids = await selectedIds(s, async (day) => {
      asked.push(day);
      return day === 'today' ? ['a', 'b', 'c'] : ['y1', 'y2'];
    });
    expect(asked).toEqual(['today']);
    expect(ids.sort()).toEqual(['a', 'c', 'y2']);
    expect(dayCount(s, days[1])).toBe(1);
  });

  it('never changes a selection it was given', () => {
    const s = setFiles(noSelection, [a], true, days);
    setFiles(s, [b], true, days);
    setDay(s, 'yesterday', true);
    expect(totals(s, days)).toEqual({ count: 1, bytes: 100 });
    expect(isEmpty(noSelection)).toBe(true);
  });

  it('knows the folder the selected files are all in, once they are all loaded', () => {
    const inFolder = (x: FileInfo, folder: string) => ({ ...x, folder }) as FileInfo;
    const loaded = [inFolder(a, 'fam'), inFolder(b, 'fam'), inFolder(y1, 'fam'), inFolder(y2, 'tax')];
    expect(commonFolder(setFiles(noSelection, [a, b, y1], true, days), loaded, days)).toBe('fam');
    expect(commonFolder(setFiles(noSelection, [a, y2], true, days), loaded, days)).toBeNull();
    // Today's third file isn't loaded: where it is, the page can't tell.
    expect(commonFolder(setDay(noSelection, 'today', true), loaded, days)).toBeNull();
    expect(commonFolder(noSelection, loaded, days)).toBeNull();
  });
});
