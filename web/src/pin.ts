// PIN input rules, identical to the server's (contract/pin_codes.json).

export const pinAlphabet = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ';
export const pinLength = 5;

/** The characters of a typed PIN that count: capitals, no spaces or dashes, only the alphabet. */
export function cleanPinInput(input: string): string {
  let out = '';
  for (const ch of input.toUpperCase()) {
    if (pinAlphabet.includes(ch)) out += ch;
  }
  return out.slice(0, pinLength);
}

/** What was typed into [before] to make [after]: only the new characters when some were added,
 * wherever the cursor was, otherwise [after] itself (a deletion or a replacement). */
export function typedInto(before: string, after: string): string {
  if (after.length <= before.length) return after;
  let start = 0;
  while (start < before.length && before[start] === after[start]) start++;
  let end = 0;
  while (end < before.length - start && before[before.length - 1 - end] === after[after.length - 1 - end]) end++;
  return after.slice(start, after.length - end);
}

/** The normalized code, or null if the input can't be a PIN (that is never sent as a try). */
export function normalizePin(input: string): string | null {
  let out = '';
  for (const ch of input.toUpperCase()) {
    if (ch === ' ' || ch === '-' || ch === '\t') continue;
    if (!pinAlphabet.includes(ch)) return null;
    out += ch;
  }
  return out.length === pinLength ? out : null;
}

/** A PIN from the page address: share.example.com/#K7M2Q, also with the secret of a PIN that
 * shows an encrypted folder after a dot (#K7M2Q.<secret>). */
export function pinFromHash(hash: string): string | null {
  return normalizePin(decodeURIComponent(hash.replace(/^#/, '').split('.')[0]));
}

/** The secret in a PIN's link: 32 bytes in base64url, 43 characters. */
export function pinSecretFromHash(hash: string): string | null {
  return /^#?[^.#]+\.([A-Za-z0-9_-]{43})$/.exec(hash)?.[1] ?? null;
}

/** The root's fingerprint in the link of a PIN that only sends into a folder with keys: 16 bytes
 * in base64url, 22 characters. */
export function pinRootFromHash(hash: string): string | null {
  return /^#?[^.#]+\.([A-Za-z0-9_-]{22})$/.exec(hash)?.[1] ?? null;
}
