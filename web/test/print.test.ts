import { describe, expect, it } from 'vitest';
import accountDe from '../src/account/i18n/de.json';
import accountEn from '../src/account/i18n/en.json';
import accountIt from '../src/account/i18n/it.json';
import { longTitle, pageTexts, type Printed } from '../src/account/admin/print';
import { addDictionaries } from '../src/i18n';

addDictionaries({ en: accountEn, de: accountDe, it: accountIt });

const printed = (p: Partial<Printed>): Printed => ({
  kind: 'poster',
  title: 'Anna & Marco',
  line: 'Share your photos of today with us',
  day: '2026-10-10',
  code: 'ANNA5',
  link: 'https://share.example.com/#ANNA5',
  brand: 'Share',
  lang: 'en',
  showsFolder: false,
  ...p,
});

describe('a PIN printed for guests', () => {
  it("speaks the server's language, whatever the admin's is", () => {
    const de = pageTexts(printed({ lang: 'de' }));
    expect(de.date).toBe('Samstag, 10. Oktober 2026');
    expect(de.steps).toEqual(['Code scannen', 'Fotos und Videos auswählen', 'Fertig, in voller Qualität']);
    expect(de.orType).toBe('Kein QR-Leser? Öffne share.example.com und tippe:');
    expect(pageTexts(printed({ lang: 'it' })).scan).toBe('Inquadralo con la fotocamera del telefono');
  });

  it('names the address to type the code into, the one the link opens, without its secret', () => {
    const texts = pageTexts(printed({ link: `https://photos.example.org:8443/#ANNA5.${'s'.repeat(43)}` }));
    expect(texts.orType).toBe('No QR reader? Open photos.example.org:8443 and type:');
  });

  it('tells guests to come back only where the PIN shows its folder', () => {
    expect(pageTexts(printed({})).note).toBe('No app, no account.');
    expect(pageTexts(printed({ showsFolder: true })).note).toBe("No app, no account. Scan again later to see everyone's photos.");
  });

  it('names a day only when one is chosen', () => {
    expect(pageTexts(printed({ day: null })).date).toBeNull();
  });

  it('gives a long title smaller letters', () => {
    expect(longTitle('Anna & Marco')).toBe(false);
    expect(longTitle('Wedding of Anna and Marco, Bolzano')).toBe(true);
  });
});
