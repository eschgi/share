import { describe, expect, it } from 'vitest';
import boot from '../public/theme-boot.js?raw';
import { darkThemes, isThemeChoice, lightThemes, metaFor, resolveTheme, themeColors, type Theme } from '../src/theme';
import css from '../src/themes.css?raw';

/** The custom properties that each rule of themes.css sets, by its theme. */
function themeBlocks(): Map<string, Map<string, string>> {
  const out = new Map<string, Map<string, string>>();
  for (const [, selector, body] of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const name = /data-theme='(\w+)'/.exec(selector)?.[1];
    if (!name) continue;
    const props = new Map<string, string>();
    for (const [, prop, value] of body.matchAll(/(--[\w-]+|color-scheme):\s*([^;]+);/g)) props.set(prop, value.trim());
    out.set(name, props);
  }
  return out;
}

describe('themes', () => {
  const blocks = themeBlocks();
  const ember = blocks.get('ember')!;

  it('sets every colour in every theme', () => {
    expect([...blocks.keys()].sort()).toEqual([...darkThemes, ...lightThemes, 'auto'].sort());
    for (const [name, props] of blocks) {
      expect([...props.keys()].sort(), name).toEqual([...ember.keys()].sort());
    }
  });

  it("paints the page in the colour the browser's bars get", () => {
    for (const name of Object.keys(themeColors) as Theme[]) {
      expect(blocks.get(name)!.get('--bg'), name).toBe(themeColors[name]);
      expect(blocks.get(name)!.get('color-scheme'), name).toBe(metaFor(name).colorScheme);
    }
  });

  it('makes Automatic Linen by day', () => {
    expect(blocks.get('auto')).toEqual(blocks.get('linen'));
    expect(resolveTheme('auto', true)).toBe('linen');
    expect(resolveTheme('auto', false)).toBe('ember');
    expect(resolveTheme('frost', false)).toBe('frost');
  });

  it('knows its choices', () => {
    expect(isThemeChoice('plum')).toBe(true);
    expect(isThemeChoice('auto')).toBe(true);
    expect(isThemeChoice('toString')).toBe(false);
    expect(isThemeChoice(null)).toBe(false);
  });
});

describe('theme-boot.js', () => {
  /** Runs the boot script against a stand-in page and reports what it set. */
  function run(stored: string | null, prefersLight: boolean) {
    const set: Record<string, string> = {};
    const element = (key: string) => ({ setAttribute: (_: string, v: string) => (set[key] = v) });
    const document = {
      documentElement: element('theme'),
      querySelector: (sel: string) => element(sel.includes('theme-color') ? 'color' : 'scheme'),
    };
    const localStorage = { getItem: () => stored };
    const matchMedia = () => ({ matches: prefersLight });
    new Function('document', 'localStorage', 'matchMedia', boot)(document, localStorage, matchMedia);
    return set;
  }

  it('says the same as theme.ts', () => {
    for (const stored of [null, 'auto', 'nonsense', '__proto__', ...darkThemes, ...lightThemes]) {
      for (const light of [false, true]) {
        const choice = isThemeChoice(stored) ? stored : 'ember';
        const meta = metaFor(resolveTheme(choice, light));
        expect(run(stored, light), `${stored}, light ${light}`).toEqual({ theme: choice, color: meta.themeColor, scheme: meta.colorScheme });
      }
    }
  });
});
