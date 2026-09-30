import { describe, expect, it } from 'vitest';
import { RateMeter, formatBytes, formatETA, formatWait, formatWhen } from '../src/format';

describe('formatBytes', () => {
  it('uses decimal units with one decimal below 10', () => {
    expect(formatBytes(1_400_000_000, 'en')).toBe('1.4 GB');
    expect(formatBytes(520_000_000, 'en')).toBe('520 MB');
    expect(formatBytes(480_000, 'en')).toBe('480 KB');
    expect(formatBytes(12, 'en')).toBe('12 B');
  });
  it('uses the decimal comma in German and Italian', () => {
    expect(formatBytes(1_400_000_000, 'de')).toBe('1,4 GB');
    expect(formatBytes(3_100_000, 'it')).toBe('3,1 MB');
  });
});

describe('formatETA and formatWait', () => {
  it('rounds up to minutes and hours', () => {
    expect(formatETA(null, 'en')).toBe('Working out the time…');
    expect(formatETA(30, 'en')).toBe('Less than a minute left');
    expect(formatETA(150, 'de')).toBe('Noch etwa 3 Min.');
    expect(formatETA(3 * 3600 + 60, 'it')).toBe('Ancora 3 h 1 min circa');
    expect(formatWait(600, 'en')).toBe('10 min');
    expect(formatWait(9.2, 'de')).toBe('10 Sek.');
  });
});

describe('formatWhen', () => {
  const now = new Date(2026, 8, 29, 21, 14);
  it('says today and tomorrow in words', () => {
    expect(formatWhen(new Date(2026, 8, 30, 18, 30), now, 'en')).toBe('tomorrow, 18:30');
    expect(formatWhen(new Date(2026, 8, 29, 23, 5), now, 'it')).toBe('oggi alle 23:05');
    expect(formatWhen(new Date(2026, 8, 30, 18, 30), now, 'de')).toBe('morgen, 18:30');
  });
  it('gives a date further out', () => {
    const later = new Date(2026, 9, 3, 9, 0);
    expect(formatWhen(later, now, 'en')).toBe('Sat, Oct 3, 09:00');
    expect(formatWhen(later, now, 'de')).toBe('Sa., 3. Okt., 09:00');
    expect(formatWhen(later, now, 'it')).toBe('sab 3 ott alle 09:00');
  });
});

describe('RateMeter', () => {
  it('estimates from a few seconds of progress', () => {
    const m = new RateMeter();
    m.add(0, 0);
    expect(m.eta(1000)).toBeNull();
    m.add(4000, 4_000_000); // 1 MB/s
    expect(m.eta(60_000_000)).toBeCloseTo(60);
  });
});
