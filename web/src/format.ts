// Numbers, sizes, durations and times, in the page's language.
import { translate, type Lang } from './i18n';

const units = ['B', 'KB', 'MB', 'GB', 'TB'];

/** 1.4 GB, 520 MB, 480 KB — decimal units, one decimal below 10. */
export function formatBytes(bytes: number, lang: Lang): string {
  let v = Math.max(bytes, 0);
  let u = 0;
  while (v >= 1000 && u < units.length - 1) {
    v /= 1000;
    u++;
  }
  const digits = u > 0 && v < 10 ? 1 : 0;
  const n = new Intl.NumberFormat(lang, { maximumFractionDigits: digits, minimumFractionDigits: digits }).format(v);
  return `${n} ${units[u]}`;
}

export function formatCount(n: number, lang: Lang): string {
  return new Intl.NumberFormat(lang).format(n);
}

/** A wait such as "10 min" or "45 s". */
export function formatWait(seconds: number, lang: Lang): string {
  if (seconds >= 60) return translate(lang, 'duration.minutes', { n: Math.ceil(seconds / 60) });
  return translate(lang, 'duration.seconds', { n: Math.max(Math.ceil(seconds), 1) });
}

/** Time left for an upload, or null while there isn't enough to go on yet. */
export function formatETA(seconds: number | null, lang: Lang): string {
  if (seconds === null || !Number.isFinite(seconds)) return translate(lang, 'sending.eta.calculating');
  if (seconds < 60) return translate(lang, 'sending.eta.seconds');
  const minutes = Math.ceil(seconds / 60);
  if (minutes < 60) return translate(lang, 'sending.eta.minutes', { n: minutes });
  return translate(lang, 'sending.eta.hours', { h: Math.floor(minutes / 60), m: minutes % 60 });
}

function sameDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}

/** When a 24-hour PIN ends: "tomorrow, 18:30", "today, 09:15" or "Sat 3 Oct, 18:30". */
export function formatWhen(when: Date, now: Date, lang: Lang): string {
  const time = new Intl.DateTimeFormat(lang, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(when);
  if (sameDay(when, now)) return translate(lang, 'when.today', { time });
  const tomorrow = new Date(now);
  tomorrow.setDate(now.getDate() + 1);
  if (sameDay(when, tomorrow)) return translate(lang, 'when.tomorrow', { time });
  const date = new Intl.DateTimeFormat(lang, { weekday: 'short', day: 'numeric', month: 'short' }).format(when);
  return translate(lang, 'when.date', { date, time });
}

/** Rolling upload speed, for a calm time estimate instead of one that jumps with every chunk. */
export class RateMeter {
  private samples: { t: number; bytes: number }[] = [];
  constructor(private readonly windowMs = 30_000) {}

  add(t: number, bytes: number): void {
    const last = this.samples[this.samples.length - 1];
    if (last && bytes < last.bytes) this.samples = []; // a new batch started over
    this.samples.push({ t, bytes });
    while (this.samples.length > 2 && t - this.samples[0].t > this.windowMs) this.samples.shift();
  }

  /** Bytes per second, or null until there are a few seconds to measure. */
  rate(): number | null {
    if (this.samples.length < 2) return null;
    const first = this.samples[0];
    const last = this.samples[this.samples.length - 1];
    const dt = (last.t - first.t) / 1000;
    if (dt < 3) return null;
    return (last.bytes - first.bytes) / dt;
  }

  eta(remaining: number): number | null {
    const r = this.rate();
    if (r === null || r <= 0) return null;
    return remaining / r;
  }
}
