// Translations: flat keys, {placeholders}, plurals as key.one / key.other.
import { createContext } from 'preact';
import { useContext } from 'preact/hooks';
import en from './en.json';
import de from './de.json';
import it from './it.json';

export const languages = ['en', 'de', 'it'] as const;
export type Lang = (typeof languages)[number];

const dictionaries: Record<Lang, Record<string, string>> = { en, de, it };

const storageKey = 'share.language';

export function isLang(v: unknown): v is Lang {
  return typeof v === 'string' && (languages as readonly string[]).includes(v);
}

/**
 * The language to show: the one picked with the globe switch, else the first browser
 * language the server offers, else the server's default.
 */
export function pickLanguage(offered: readonly string[], stored: string | null, browser: readonly string[], fallback: string): Lang {
  const allowed = offered.filter(isLang);
  if (stored && isLang(stored) && allowed.includes(stored)) return stored;
  for (const tag of browser) {
    const base = tag.toLowerCase().split('-')[0];
    if (isLang(base) && allowed.includes(base)) return base;
  }
  if (isLang(fallback) && allowed.includes(fallback)) return fallback;
  return allowed[0] ?? 'en';
}

export function storedLanguage(): string | null {
  try {
    return localStorage.getItem(storageKey);
  } catch {
    return null;
  }
}

export function storeLanguage(lang: Lang): void {
  try {
    localStorage.setItem(storageKey, lang);
  } catch {
    // private mode: the choice just isn't remembered
  }
}

export type Params = Record<string, string | number>;

/** Looks up key in lang, falling back to English, then to the key itself. */
export function translate(lang: Lang, key: string, params?: Params): string {
  const text = dictionaries[lang][key] ?? dictionaries.en[key] ?? key;
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (m, name: string) => (name in params ? String(params[name]) : m));
}

/** Translates a plural key: key.one or key.other by the language's plural rules. */
export function translatePlural(lang: Lang, key: string, n: number, params?: Params): string {
  const form = new Intl.PluralRules(lang).select(n);
  const full = `${key}.${form}`;
  const k = full in dictionaries[lang] || full in dictionaries.en ? full : `${key}.other`;
  return translate(lang, k, { n, ...params });
}

/** Every key must exist in every language; the tests use this. */
export function missingKeys(): Record<Lang, string[]> {
  const all = new Set(languages.flatMap((l) => Object.keys(dictionaries[l])));
  const out = {} as Record<Lang, string[]>;
  for (const l of languages) out[l] = [...all].filter((k) => !(k in dictionaries[l]));
  return out;
}

export interface I18n {
  lang: Lang;
  offered: Lang[];
  setLang: (l: Lang) => void;
  t: (key: string, params?: Params) => string;
  tn: (key: string, n: number, params?: Params) => string;
}

export function makeI18n(lang: Lang, offered: Lang[], setLang: (l: Lang) => void): I18n {
  return {
    lang,
    offered,
    setLang,
    t: (key, params) => translate(lang, key, params),
    tn: (key, n, params) => translatePlural(lang, key, n, params),
  };
}

export const I18nContext = createContext<I18n>(makeI18n('en', ['en'], () => {}));

export function useI18n(): I18n {
  return useContext(I18nContext);
}
