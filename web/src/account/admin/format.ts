// How the admin pages say things about people and their phones and browsers.
import type { ListedDevice, Person } from '../../api';
import { daysAgo } from '../../format';
import type { I18n } from '../../i18n';

/** "1 phone, 1 browser", "2 phones", or that there is none. */
export function devicesText(i18n: Pick<I18n, 't' | 'tn'>, devices: readonly ListedDevice[]): string {
  const phones = devices.filter((d) => d.client === 'app').length;
  const browsers = devices.length - phones;
  if (devices.length === 0) return i18n.t('people.noDevices');
  return [phones > 0 ? i18n.tn('people.phones', phones) : '', browsers > 0 ? i18n.tn('people.browsers', browsers) : '']
    .filter(Boolean)
    .join(', ');
}

/** The line under a person in the list: "You · 1 phone", or "2 phones · active today". */
export function personLine(i18n: Pick<I18n, 't' | 'tn'>, p: Person, now: Date): string {
  const devices = devicesText(i18n, p.phones);
  if (p.me) return i18n.t('people.you', { devices });
  if (!p.last_seen_at) return devices;
  const days = daysAgo(new Date(p.last_seen_at), now);
  const active = days === 0 ? i18n.t('people.activeToday') : days === 1 ? i18n.t('people.activeYesterday') : i18n.t('people.activeDays', { n: days });
  return `${devices} · ${active}`;
}

/** Whole days until a deleted file goes for good: 0 on its last day. */
export function daysLeft(purgeAt: Date, now: Date): number {
  return Math.max(0, Math.floor((purgeAt.getTime() - now.getTime()) / 86_400_000));
}
