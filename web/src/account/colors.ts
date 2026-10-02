// Colours picked from an id, the same way the app picks them (app/lib/ui/theme.dart), so a
// person or a photo looks the same on the phone and in the browser.

function hash(id: string): number {
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (Math.imul(h, 31) + id.charCodeAt(i)) & 0x7fffffff;
  return h;
}

/** The avatar's colour: one of three, as class names (account.css). */
export function avatarClass(id: string): string {
  return ['acc', 'tan', 'sage'][hash(id) % 3];
}

/** A photo's or video's placeholder tone, t1 to t10 (styles.css), until its thumbnail is there. */
export function toneClass(id: string): string {
  return `t${(hash(id) % 10) + 1}`;
}

/** The letter in someone's avatar. */
export function initial(name: string): string {
  return ([...name.trim()][0] ?? '?').toLocaleUpperCase();
}
