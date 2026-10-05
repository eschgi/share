import { describe, expect, it } from 'vitest';
import { doneBytes, linkLifeMs, missingParts, partSpan, s3FailureAction, Slots, stale } from '../src/s3parts';

describe('the parts of a file', () => {
  it('knows where each part is and what is left', () => {
    expect(partSpan(1, 25, 10)).toEqual([0, 10]);
    expect(partSpan(3, 25, 10)).toEqual([20, 25]);
    expect(missingParts(3, [2])).toEqual([1, 3]);
    expect(missingParts(0, [])).toEqual([]);
    expect(doneBytes([1, 3, 3], 25, 10)).toBe(15);
  });

  it('asks for links again before they run out', () => {
    expect(stale(1000, 1000 + linkLifeMs - 1)).toBe(false);
    expect(stale(1000, 1000 + linkLifeMs)).toBe(true);
  });

  it('knows what a failure means', () => {
    expect(s3FailureAction('bucket', 0)).toBe('refresh');
    expect(s3FailureAction('bucket', 403)).toBe('refresh');
    expect(s3FailureAction('bucket', 404)).toBe('resync');
    expect(s3FailureAction('bucket', 503)).toBe('retry');
    expect(s3FailureAction('server', 0)).toBe('retry');
    expect(s3FailureAction('server', 503, 's3_unavailable')).toBe('retry');
    expect(s3FailureAction('server', 409, 's3_parts_missing')).toBe('resync');
    expect(s3FailureAction('server', 409, 's3_upload_finished')).toBe('resync');
    expect(s3FailureAction('server', 404, 'not_found')).toBe('restart');
    expect(s3FailureAction('server', 404, 'folder_gone')).toBe('refuse');
    expect(s3FailureAction('server', 401, 'session_ended')).toBe('refuse');
    expect(s3FailureAction('server', 413, 'too_large')).toBe('refuse');
  });

  it('runs at most so many at once', async () => {
    const slots = new Slots(2);
    const order: string[] = [];
    const work = async (name: string, ms: number) => {
      const give = await slots.take();
      order.push(name + ' starts');
      await new Promise((r) => setTimeout(r, ms));
      order.push(name + ' ends');
      give();
      give(); // twice changes nothing
    };
    await Promise.all([work('a', 20), work('b', 5), work('c', 1)]);
    expect(order.slice(0, 2)).toEqual(['a starts', 'b starts']);
    expect(order.indexOf('c starts')).toBe(order.indexOf('b ends') + 1);
  });
});
