import { describe, expect, it } from 'vitest';
import type { FolderInfo, OpenInvite, People, Person } from '../src/api';
import { allTotals, dropTarget, hasChoices, sendTarget, validShown, whoSees } from '../src/account/folders/folders';
import { uploadMeta } from '../src/uploader';

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

describe('sending into folders', () => {
  const list = [folder('family'), folder('wedding'), folder('kindergarten')];

  it('goes into the folder chosen, else the one shown, else the oldest', () => {
    expect(sendTarget(list, 'wedding', 'kindergarten')).toBe('kindergarten');
    expect(sendTarget(list, 'wedding', null)).toBe('wedding');
    expect(sendTarget(list, null, null)).toBe('family');
    expect(sendTarget(list, 'gone', 'gone too')).toBe('family');
    expect(sendTarget([], null, null)).toBe(null);
  });

  it('sends dropped files where a folder is in view, and asks elsewhere', () => {
    expect(dropTarget('/library', list, 'wedding', 'family')).toBe('wedding');
    expect(dropTarget('/library', list, null, 'family')).toBe('ask'); // all folders shown
    expect(dropTarget('/send', list, null, 'kindergarten')).toBe('kindergarten');
    expect(dropTarget('/settings/people', list, 'wedding', 'family')).toBe('ask');
    expect(dropTarget('/settings', [folder('family')], null, 'family')).toBe('family'); // nothing to choose
    expect(dropTarget('/library', [], null, null)).toBe('ask');
  });

  it('says the folder in an upload, and the inbox key of a shared file', () => {
    expect(uploadMeta(undefined, undefined)).toEqual({});
    expect(uploadMeta(7, 'wedding')).toEqual({ shareKey: '7', folder: 'wedding' });
    expect(uploadMeta(undefined, 'family')).toEqual({ folder: 'family' });
  });
});

describe('who sees a folder', () => {
  it('is the admins, the members given it, and open invites for new people', () => {
    const person = (id: string, role: 'admin' | 'member', folders: string[]) =>
      ({ id, name: id, role, username: null, has_password: false, me: false, created_at: '', last_seen_at: null, phones: [], folders }) as Person;
    const invite = (id: string, role: 'admin' | 'member', folders: string[], user_id: string | null = null) =>
      ({ id, name: id, role, user_id, created_at: '', expires_at: '', folders }) as OpenInvite;
    const people: People = {
      users: [person('stefan', 'admin', ['family', 'taxes']), person('maria', 'member', ['family', 'wedding']), person('peter', 'member', ['family'])],
      invites: [invite('rosa', 'member', ['wedding']), invite('marco', 'admin', []), invite('phone', 'member', ['wedding'], 'peter')],
    };
    expect(whoSees(people, 'wedding').map((s) => [s.id, s.pending])).toEqual([
      ['stefan', false],
      ['maria', false],
      ['rosa', true],
      ['marco', true],
    ]);
    expect(whoSees(people, 'taxes').map((s) => s.id)).toEqual(['stefan', 'marco']);
  });
});
