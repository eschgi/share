import { describe, expect, it } from 'vitest';
import { guestZipName } from '../src/guest/see';

describe("a guest's look into a folder", () => {
  it('names the ZIP of everything as the server does', () => {
    const now = new Date(2026, 9, 3, 12);
    expect(guestZipName('Share', ['2026-09-26'], now)).toBe('Share 2026-09-26.zip');
    expect(guestZipName('Share', ['2026-09-27', '2026-09-26'], now)).toBe('Share 2026-10-03.zip');
  });
});
