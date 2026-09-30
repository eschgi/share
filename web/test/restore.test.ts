import { describe, expect, it } from 'vitest';
import { dedupeBatches, matchGhosts, unbatched } from '../src/restore';

const ghost = (id: string, name: string, size: number, type = 'video/mp4') => ({ id, name, size, type });
const picked = (name: string, size: number, type = 'video/mp4') => ({ name, size, type });

describe('matchGhosts', () => {
  it('matches by name, size and type, whatever the order', () => {
    const ghosts = [ghost('a', 'one.mp4', 100), ghost('b', 'two.mp4', 200)];
    const two = picked('two.mp4', 200);
    const one = picked('one.mp4', 100);
    expect(matchGhosts(ghosts, [two, one])).toEqual({ matched: [['b', two], ['a', one]], rest: [] });
  });

  it('keeps files that match no ghost apart', () => {
    const other = picked('two.mp4', 201); // same name, other size: another file
    const { matched, rest } = matchGhosts([ghost('b', 'two.mp4', 200)], [other]);
    expect(matched).toEqual([]);
    expect(rest).toEqual([other]);
  });

  it('uses each ghost once, even when two files look alike', () => {
    const ghosts = [ghost('a', 'IMG.jpg', 5, 'image/jpeg'), ghost('b', 'IMG.jpg', 5, 'image/jpeg')];
    const f1 = picked('IMG.jpg', 5, 'image/jpeg');
    const f2 = picked('IMG.jpg', 5, 'image/jpeg');
    const f3 = picked('IMG.jpg', 5, 'image/jpeg');
    expect(matchGhosts(ghosts, [f1, f2, f3])).toEqual({ matched: [['a', f1], ['b', f2]], rest: [f3] });
  });

  it('accepts a missing type on either side', () => {
    const f = picked('scan.pdf', 9, '');
    expect(matchGhosts([ghost('a', 'scan.pdf', 9, 'application/pdf')], [f]).matched).toEqual([['a', f]]);
  });
});

describe('dedupeBatches', () => {
  it('keeps a retried file only in its newest batch', () => {
    const batches = {
      first: { fileIDs: ['a', 'b', 'c'], step: 1 },
      retry: { fileIDs: ['b'], step: 0 },
    };
    expect(dedupeBatches(batches)).toEqual({
      first: { fileIDs: ['a', 'c'], step: 1 },
      retry: { fileIDs: ['b'], step: 0 },
    });
  });

  it('drops batches that have nothing left', () => {
    expect(dedupeBatches({ old: { fileIDs: ['a'] }, newer: { fileIDs: ['a'] } })).toEqual({ newer: { fileIDs: ['a'] } });
  });
});

describe('unbatched', () => {
  const file = (id: string, started: boolean, complete = false, isGhost = false) => ({
    id,
    isGhost,
    progress: { uploadStarted: started ? 1 : null, uploadComplete: complete },
  });

  it('finds started, unfinished files that no batch runs', () => {
    const files = [
      file('failed', true), // its batch finished without it
      file('running', true), // still in a batch
      file('done', true, true),
      file('ghost', true, false, true), // waits to be picked again
      file('new', false), // not started: upload() takes it
    ];
    expect(unbatched(files, { b: { fileIDs: ['running'] } })).toEqual(['failed']);
  });
});
