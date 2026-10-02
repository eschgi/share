import { describe, expect, it } from 'vitest';
import { avatarClass, initial, toneClass } from '../src/account/colors';

describe('colours picked from an id', () => {
  // Worked out with the app's rule (ShareColors._hash in app/lib/ui/theme.dart), so a person
  // or a photo has the same colour in the app and in the browser.
  it('are the ones the app picks', () => {
    expect(avatarClass('u7ld5x2k7mbqz4bwdbyj6qsqxa')).toBe('acc');
    expect(avatarClass('u2ld5x2k7mbqz4bwdbyj6qsqxa')).toBe('tan');
    expect(avatarClass('u3ld5x2k7mbqz4bwdbyj6qsqxa')).toBe('sage');
    expect(toneClass('u7ld5x2k7mbqz4bwdbyj6qsqxa')).toBe('t8');
    expect(toneClass('u2ld5x2k7mbqz4bwdbyj6qsqxa')).toBe('t3');
    expect(toneClass('f9xk2m4q7r1t3v5w8y0z2b4d6')).toBe('t7');
    expect(toneClass('')).toBe('t1');
  });
});

describe('initial', () => {
  it('is the first letter, in capitals', () => {
    expect(initial('maria')).toBe('M');
    expect(initial('  öma Rosa')).toBe('Ö');
    expect(initial('😀 Smile')).toBe('😀');
    expect(initial('')).toBe('?');
  });
});
