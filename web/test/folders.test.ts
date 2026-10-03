import { describe, expect, it } from 'vitest';
import type { FolderInfo } from '../src/api';
import { allTotals, hasChoices, validShown } from '../src/account/folders/folders';

function folder(id: string, files = 1, bytes = 100): FolderInfo {
  return { id, name: id, files, bytes, senders: 1, people: 2, admins_only: false, cover: null, created_at: '2026-09-12T08:00:00Z' };
}

describe('folders', () => {
  it('offer a choice only from the second folder on', () => {
    expect(hasChoices(null)).toBe(false);
    expect(hasChoices([folder('family')])).toBe(false);
    expect(hasChoices([folder('family'), folder('wedding')])).toBe(true);
  });

  it('show the folder chosen last time while the person still sees it', () => {
    const list = [folder('family'), folder('wedding')];
    expect(validShown(list, 'wedding')).toBe('wedding');
    expect(validShown(list, 'taxes')).toBe(null); // taken away, or deleted
    expect(validShown(list, null)).toBe(null);
    expect(validShown(null, 'wedding')).toBe(null); // not fetched yet
  });

  it('add up what all of them hold', () => {
    expect(allTotals([folder('family', 2340, 41e9), folder('wedding', 412, 9.8e9)])).toEqual({ files: 2752, bytes: 50.8e9 });
    expect(allTotals([])).toEqual({ files: 0, bytes: 0 });
  });
});
