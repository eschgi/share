import { describe, expect, it } from 'vitest';
import de from '../src/i18n/de.json';
import en from '../src/i18n/en.json';
import itDict from '../src/i18n/it.json';
import { missingKeys, pickLanguage, translate, translatePlural } from '../src/i18n';

const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

describe('translations', () => {
  it('have every key in every language', () => {
    expect(missingKeys()).toEqual({ en: [], de: [], it: [] });
  });

  it('use the same placeholders and bold markup in every language', () => {
    const dicts: Record<string, Record<string, string>> = { de, it: itDict };
    for (const [key, text] of Object.entries(en as Record<string, string>)) {
      for (const [lang, dict] of Object.entries(dicts)) {
        expect(placeholders(dict[key]), `${lang} ${key}`).toEqual(placeholders(text));
        expect(dict[key].includes('<b>'), `${lang} ${key}`).toBe(text.includes('<b>'));
      }
    }
  });

  it('fill in placeholders and plurals', () => {
    expect(translate('de', 'sending.of', { total: 100 })).toBe('von 100');
    expect(translatePlural('en', 'pin.wrong', 1)).toBe("That PIN didn't work. 1 try left.");
    expect(translatePlural('it', 'pin.wrong', 3)).toBe('Questo PIN non ha funzionato. Ancora 3 tentativi.');
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
