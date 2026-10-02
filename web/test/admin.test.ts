import { describe, expect, it } from 'vitest';
import type { ListedDevice, Person } from '../src/api';
import accountDe from '../src/account/i18n/de.json';
import accountEn from '../src/account/i18n/en.json';
import accountIt from '../src/account/i18n/it.json';
import { daysLeft, devicesText, personLine } from '../src/account/admin/format';
import { addDictionaries, translate, translatePlural, type Lang } from '../src/i18n';

addDictionaries({ en: accountEn, de: accountDe, it: accountIt });
const i18n = (lang: Lang) => ({
  t: (key: string, p?: Record<string, string | number>) => translate(lang, key, p),
  tn: (key: string, n: number, p?: Record<string, string | number>) => translatePlural(lang, key, n, p),
});
const device = (client: 'app' | 'web') => ({ client }) as ListedDevice;
const person = (phones: ListedDevice[], me: boolean, seen: string | null) => ({ phones, me, last_seen_at: seen }) as Person;

describe('devicesText', () => {
  it('counts phones and browsers apart', () => {
    expect(devicesText(i18n('en'), [device('app'), device('web')])).toBe('1 phone, 1 browser');
    expect(devicesText(i18n('en'), [device('app'), device('app')])).toBe('2 phones');
    expect(devicesText(i18n('de'), [device('web'), device('web')])).toBe('2 Browser');
    expect(devicesText(i18n('it'), [])).toBe('Nessun telefono né browser');
  });
});

describe('personLine', () => {
  const now = new Date(2026, 9, 2, 12, 0);
  it('says it is you', () => {
    expect(personLine(i18n('en'), person([device('app'), device('web')], true, null), now)).toBe('You · 1 phone, 1 browser');
  });
  it('says when they were last active', () => {
    expect(personLine(i18n('en'), person([device('app')], false, new Date(2026, 9, 2, 8).toISOString()), now)).toBe('1 phone · active today');
    expect(personLine(i18n('de'), person([device('app')], false, new Date(2026, 9, 1, 8).toISOString()), now)).toBe('1 Handy · gestern aktiv');
    expect(personLine(i18n('it'), person([device('web')], false, new Date(2026, 8, 28, 8).toISOString()), now)).toBe('1 browser · attività 4 giorni fa');
  });
});

describe('daysLeft', () => {
  it('counts whole days, down to 0 on the last one', () => {
    const now = new Date(Date.UTC(2026, 9, 2, 12));
    expect(daysLeft(new Date(Date.UTC(2026, 10, 1, 12)), now)).toBe(30);
    expect(daysLeft(new Date(Date.UTC(2026, 9, 3, 11)), now)).toBe(0);
    expect(daysLeft(new Date(Date.UTC(2026, 9, 1)), now)).toBe(0);
  });
});
