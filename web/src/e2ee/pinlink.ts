// What a PIN's link carries after its code (docs/e2ee-plan.md): the secret of a PIN that shows an
// encrypted folder, or the root's fingerprint for one that only sends into a folder with keys. It
// never goes to the server; this browser keeps it next to the PIN's session, so the folder still
// opens and is still checked after a reload, or another day. A PIN unlocked without either
// forgets them.
const keys = { secret: 'share-pin-secret', root: 'share-pin-root' } as const;
const kept: Record<keyof typeof keys, string | null> = { secret: null, root: null };

function keep(which: keyof typeof keys, value: string | null): void {
  kept[which] = value;
  try {
    if (value) localStorage.setItem(keys[which], value);
    else localStorage.removeItem(keys[which]);
  } catch {
    // storage blocked: only this page has it
  }
}

function read(which: keyof typeof keys): string | null {
  try {
    return localStorage.getItem(keys[which]) ?? kept[which];
  } catch {
    return kept[which];
  }
}

/** Keeps what the link of the PIN just unlocked carries, and forgets what an earlier one did. */
export function keepPinLink(secret: string | null, root: string | null): void {
  keep('secret', secret);
  keep('root', root);
}

export const pinSecret = () => read('secret');

/** The root's fingerprint the PIN's link carried. */
export const pinRoot = () => read('root');

export function forgetPinLink(): void {
  keepPinLink(null, null);
}
