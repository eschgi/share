import { describe, expect, it } from 'vitest';
import de from '../src/i18n/de.json';
import en from '../src/i18n/en.json';
import itDict from '../src/i18n/it.json';
import accountDe from '../src/account/i18n/de.json';
import accountEn from '../src/account/i18n/en.json';
import accountIt from '../src/account/i18n/it.json';
import { addDictionaries, missingKeys, pickLanguage, translate, translatePlural } from '../src/i18n';

const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

function samePlaceholders(en: Record<string, string>, others: Record<string, Record<string, string>>) {
  for (const [key, text] of Object.entries(en)) {
    for (const [lang, dict] of Object.entries(others)) {
      expect(placeholders(dict[key]), `${lang} ${key}`).toEqual(placeholders(text));
      expect(dict[key].includes('<b>'), `${lang} ${key}`).toBe(text.includes('<b>'));
    }
  }
}

describe('translations', () => {
  it('have every key in every language', () => {
    expect(missingKeys()).toEqual({ en: [], de: [], it: [] });
  });

  it('use the same placeholders and bold markup in every language', () => {
    samePlaceholders(en, { de, it: itDict });
  });

  it('fill in placeholders and plurals', () => {
    expect(translate('de', 'sending.of', { total: 100 })).toBe('von 100');
    expect(translatePlural('en', 'pin.wrong', 1)).toBe("That PIN didn't work. 1 try left.");
    expect(translatePlural('it', 'pin.wrong', 3)).toBe('Questo PIN non ha funzionato. Ancora 3 tentativi.');
  });
});

// The account's pages bring their own texts (src/account/i18n), added when they load.
describe("the account's translations", () => {
  it('have every key in every language, with the same placeholders', () => {
    for (const dict of [accountDe, accountIt]) expect(Object.keys(dict).sort()).toEqual(Object.keys(accountEn).sort());
    samePlaceholders(accountEn, { de: accountDe, it: accountIt });
  });

  it("leave the PIN pages' texts as they are", () => {
    expect(Object.keys(accountEn).filter((k) => k in en)).toEqual([]);
  });

  it('are there once added', () => {
    addDictionaries({ en: accountEn, de: accountDe, it: accountIt });
    expect(missingKeys()).toEqual({ en: [], de: [], it: [] });
    expect(translatePlural('de', 'devices.lastUsedDays', 3)).toBe('Zuletzt benutzt vor 3 Tagen');
    expect(translatePlural('it', 'signIn.wrongLeft', 1)).toBe('Nome utente o password errati. Ancora 1 tentativo.');
  });
});

describe('translatePlural', () => {
  it('says none with the zero form, where a text has one', () => {
    addDictionaries({
      en: { 'test.files.zero': 'No files', 'test.files.one': '1 file', 'test.files.other': '{n} files' },
      de: { 'test.files.zero': 'Keine Dateien', 'test.files.one': '1 Datei', 'test.files.other': '{n} Dateien' },
      it: { 'test.files.zero': 'Nessun file', 'test.files.one': '1 file', 'test.files.other': '{n} file' },
    });
    expect(translatePlural('en', 'test.files', 0)).toBe('No files');
    expect(translatePlural('de', 'test.files', 0)).toBe('Keine Dateien');
    expect(translatePlural('it', 'test.files', 1)).toBe('1 file');
    expect(translatePlural('de', 'test.files', 2)).toBe('2 Dateien');
    expect(translatePlural('en', 'pin.wrong', 0)).toBe("That PIN didn't work. 0 tries left.");
  });
});

describe('pickLanguage', () => {
  const all = ['en', 'de', 'it'];
  it('prefers the stored choice', () => {
    expect(pickLanguage(all, 'it', ['de-DE'], 'en')).toBe('it');
  });
  it('then the first browser language on offer', () => {
    expect(pickLanguage(all, null, ['fr-FR', 'de-AT', 'en'], 'en')).toBe('de');
  });
  it('then the default', () => {
    expect(pickLanguage(all, null, ['fr-FR'], 'it')).toBe('it');
  });
  it('ignores languages the server does not offer', () => {
    expect(pickLanguage(['en', 'de'], 'it', ['it-IT'], 'en')).toBe('en');
  });
});
