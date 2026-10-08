// What a PIN's printed page says (screens 71 and 72), apart from the page itself, for the tests.

import { formatLongDay } from '../../format';
import { translate, type Lang } from '../../i18n';

/** A poster for a wall, or four cards to cut out for the tables. */
export type PrintKind = 'poster' | 'cards';

/** What goes on the page: the form's choices, and the PIN's link and code. */
export interface Printed {
  kind: PrintKind;
  title: string;
  line: string;
  /** The day the poster names, 2026-10-10, or none. */
  day: string | null;
  /** The code to type, or none. */
  code: string | null;
  link: string;
  /** The server's name, at the top of a poster. */
  brand: string;
  /** The server's language, which guests see first. */
  lang: Lang;
  showsFolder: boolean;
}

/** The page's words, in its language: guests who type the code open the PIN's own address. */
export function pageTexts(p: Printed) {
  const t = (key: string, params?: Record<string, string>) => translate(p.lang, key, params);
  return {
    date: p.day ? formatLongDay(p.day, p.lang) : null,
    scan: t('print.scan'),
    steps: [t('print.step1'), t('print.step2'), t('print.step3')],
    // A PIN that shows its folder lets guests come back for everyone's photos.
    note: p.showsFolder ? `${t('print.noApp')} ${t('print.seeLater')}` : t('print.noApp'),
    orType: t('print.orType', { host: new URL(p.link).host }),
  };
}

/** A long title gets smaller letters, so it still fits in two or three lines. */
export function longTitle(title: string): boolean {
  return title.length > 22;
}
