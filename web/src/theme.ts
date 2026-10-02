// The app's themes, chosen per browser and kept in localStorage like the language. Before the
// page is drawn, public/theme-boot.js applies the stored one, so nothing flashes in another
// theme; this keeps it right afterwards, when the choice changes or Automatic meets night or
// day. theme-boot.js repeats the rules below; test/theme.test.ts keeps them equal.

export const darkThemes = ['ember', 'midnight', 'moss', 'plum', 'black'] as const;
export const lightThemes = ['linen', 'frost'] as const;
export type Theme = (typeof darkThemes)[number] | (typeof lightThemes)[number];
/** A theme, or "auto": Linen by day, Ember at night, as the system says. */
export type ThemeChoice = Theme | 'auto';

/** Each theme's page colour, for the browser's own bars (meta theme-color). */
export const themeColors: Record<Theme, string> = {
  ember: '#16120f',
  midnight: '#0f141b',
  moss: '#111512',
  plum: '#151118',
  black: '#000000',
  linen: '#f6f0e9',
  frost: '#f3f6fa',
};

const storageKey = 'share.theme';
const lightQuery = '(prefers-color-scheme: light)';

export function isThemeChoice(v: unknown): v is ThemeChoice {
  return v === 'auto' || (typeof v === 'string' && Object.hasOwn(themeColors, v));
}

/** The theme chosen in this browser; Ember when none was, as in the app. */
export function storedTheme(): ThemeChoice {
  try {
    const v = localStorage.getItem(storageKey);
    return isThemeChoice(v) ? v : 'ember';
  } catch {
    return 'ember';
  }
}

export function storeTheme(choice: ThemeChoice): void {
  try {
    if (choice === 'ember') localStorage.removeItem(storageKey);
    else localStorage.setItem(storageKey, choice);
  } catch {
    // Without storage the choice lasts as long as the page.
  }
}

/** The theme a choice shows now. */
export function resolveTheme(choice: ThemeChoice, prefersLight: boolean): Theme {
  if (choice === 'auto') return prefersLight ? 'linen' : 'ember';
  return choice;
}

/** What the browser's own bars and controls should look like in a theme. */
export function metaFor(theme: Theme): { themeColor: string; colorScheme: 'light' | 'dark' } {
  const light = (lightThemes as readonly string[]).includes(theme);
  return { themeColor: themeColors[theme], colorScheme: light ? 'light' : 'dark' };
}

/** Shows a choice on this page: data-theme on <html> for the colours, the meta tags for the
 * browser's bars. */
export function applyTheme(choice: ThemeChoice = storedTheme()): void {
  const { themeColor, colorScheme } = metaFor(resolveTheme(choice, matchMedia(lightQuery).matches));
  document.documentElement.setAttribute('data-theme', choice);
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', themeColor);
  document.querySelector('meta[name="color-scheme"]')?.setAttribute('content', colorScheme);
}

/** Keeps the theme right while the page is open: when the system turns to day or night, and
 * when another tab chooses another theme. */
export function watchTheme(): void {
  matchMedia(lightQuery).addEventListener('change', () => applyTheme());
  addEventListener('storage', (e) => {
    if (e.key === storageKey || e.key === null) applyTheme();
  });
}
