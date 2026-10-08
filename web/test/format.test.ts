import { describe, expect, it } from 'vitest';
import { RateMeter, dayOf, daysAgo, formatBytes, formatDay, formatDuration, formatETA, formatLongDay, formatPercent, formatWait, formatWhen } from '../src/format';

describe('formatPercent', () => {
  it('writes the share the way each language does', () => {
    expect(formatPercent(0.376, 'en')).toBe('37%');
    expect(formatPercent(0.376, 'de')).toBe('37\u00a0%');
    expect(formatPercent(0.376, 'it')).toBe('37%');
  });
  it('only says 100% when everything is there', () => {
    expect(formatPercent(0.999, 'en')).toBe('99%');
    expect(formatPercent(1, 'en')).toBe('100%');
    expect(formatPercent(0, 'en')).toBe('0%');
  });
});

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

describe('daysAgo', () => {
  it('counts calendar days here, not periods of 24 hours', () => {
    const now = new Date(2026, 9, 2, 0, 30);
    expect(daysAgo(new Date(2026, 9, 2, 0, 5), now)).toBe(0);
    expect(daysAgo(new Date(2026, 9, 1, 23, 50), now)).toBe(1);
    expect(daysAgo(new Date(2026, 8, 25, 12, 0), now)).toBe(7);
  });
  it('counts across a change of the clocks', () => {
    expect(daysAgo(new Date(2026, 2, 28, 12, 0), new Date(2026, 2, 30, 12, 0))).toBe(2);
    expect(daysAgo(new Date(2026, 9, 24, 12, 0), new Date(2026, 9, 26, 12, 0))).toBe(2);
  });
  it('says today for times a little ahead', () => {
    expect(daysAgo(new Date(2026, 9, 3, 9, 0), new Date(2026, 9, 2, 9, 0))).toBe(0);
  });
});

describe('formatDay', () => {
  const now = new Date(2026, 9, 2, 9, 0);
  it('says today and yesterday', () => {
    expect(formatDay('2026-10-02', now, 'en', 'Today', 'Yesterday')).toBe('Today');
    expect(formatDay('2026-10-01', now, 'de', 'Heute', 'Gestern')).toBe('Gestern');
  });
  it('writes other days the way the app does', () => {
    expect(formatDay('2026-09-27', now, 'en', 'Today', 'Yesterday')).toBe('Sunday, 27 Sep');
    expect(formatDay('2026-09-27', now, 'de', 'Heute', 'Gestern')).toBe('Sonntag, 27. September');
    expect(formatDay('2026-09-27', now, 'it', 'Oggi', 'Ieri')).toBe('Domenica 27 settembre');
  });
  it('adds the year when it is another one', () => {
    expect(formatDay('2025-12-31', now, 'en', 'Today', 'Yesterday')).toBe('Wednesday, 31 Dec 2025');
    expect(formatDay('2025-12-31', now, 'de', 'Heute', 'Gestern')).toBe('Mittwoch, 31. Dezember 2025');
  });
  it('does not call a day ahead of this clock today', () => {
    expect(formatDay('2026-10-03', now, 'en', 'Today', 'Yesterday')).toBe('Saturday, 3 Oct');
  });
});

describe('formatLongDay', () => {
  it('writes the day in full, as a poster names it', () => {
    expect(formatLongDay('2026-10-10', 'en')).toBe('Saturday, 10 October 2026');
    expect(formatLongDay('2026-10-10', 'de')).toBe('Samstag, 10. Oktober 2026');
    expect(formatLongDay('2026-10-10', 'it')).toBe('Sabato 10 ottobre 2026');
  });
  it('and reads it back from a date', () => {
    expect(dayOf(new Date(2026, 9, 10, 23, 59))).toBe('2026-10-10');
    expect(dayOf(new Date(2027, 0, 5))).toBe('2027-01-05');
  });
});

describe('formatDuration', () => {
  it('writes minutes and seconds, and hours when there are some', () => {
    expect(formatDuration(18_000)).toBe('0:18');
    expect(formatDuration(75_400)).toBe('1:15');
    expect(formatDuration(3_723_000)).toBe('1:02:03');
  });
});
