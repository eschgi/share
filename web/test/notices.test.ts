import { describe, expect, it } from 'vitest';
import accountDe from '../src/account/i18n/de.json';
import accountEn from '../src/account/i18n/en.json';
import accountIt from '../src/account/i18n/it.json';
import { formatBytes } from '../src/format';
import { addDictionaries, languages, translate, translatePlural } from '../src/i18n';
import { noticeText, type Notice, type Words } from '../src/notices';

addDictionaries({ en: accountEn, de: accountDe, it: accountIt });

const words = (lang: (typeof languages)[number]): Words => ({
  t: (key, params) => translate(lang, key, params),
  tn: (key, n, params) => translatePlural(lang, key, n, params),
  bytes: (b) => formatBytes(b, lang),
});

const all: Notice[] = [
  { kind: 'sent', files: 12, bytes: 3_500_000_000 },
  { kind: 'failed', failed: 2 },
  { kind: 'pinEnded' },
  { kind: 'pinLost' },
  { kind: 'signedOut' },
  { kind: 'saved', saved: 40, folder: 'Share' },
  { kind: 'saveStopped', why: 'full' },
  { kind: 'saveStopped', why: 'folder' },
  { kind: 'saveStopped', why: 'signedOut' },
];

describe('notices', () => {
  it('say what ended, in every language, with nothing left to fill in', () => {
    for (const lang of languages) {
      for (const n of all) {
        const { title, body } = noticeText(n, words(lang));
        expect(title, `${lang} ${n.kind}`).not.toMatch(/[{}]|^notice\.|^save\.|^pin\./);
        expect(body, `${lang} ${n.kind}`).not.toMatch(/[{}]|^notice\.|^save\.|^pin\./);
      }
    }
  });
  it('count and size what was sent', () => {
    expect(noticeText({ kind: 'sent', files: 12, bytes: 3_500_000_000 }, words('en'))).toEqual({
      tag: 'send',
      title: 'Your files arrived',
      body: '12 files, 3.5 GB in total.',
    });
    expect(noticeText({ kind: 'failed', failed: 1 }, words('en')).body).toBe("1 file couldn't be sent. Open the page to try again.");
  });
  it('keep sending and saving apart, so one never replaces the other', () => {
    expect(noticeText({ kind: 'pinEnded' }, words('en')).tag).toBe('send');
    expect(noticeText({ kind: 'saved', saved: 3, folder: 'Fotos' }, words('de'))).toEqual({
      tag: 'save',
      title: '3 Dateien gespeichert',
      body: 'In den Ordner „Fotos“',
    });
  });
});
