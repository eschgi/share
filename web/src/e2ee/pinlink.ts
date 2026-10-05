// The secret of the link of a PIN that shows an encrypted folder (docs/e2ee-plan.md). It never
// goes to the server; this browser keeps it next to the PIN's session, so the folder still
// opens after a reload, or another day.
const key = 'share-pin-secret';
let kept: string | null = null;

export function keepPinSecret(secret: string): void {
  kept = secret;
  try {
    localStorage.setItem(key, secret);
  } catch {
    // storage blocked: only this page has it
  }
}

export function pinSecret(): string | null {
  try {
    return localStorage.getItem(key) ?? kept;
  } catch {
    return kept;
  }
}

export function forgetPinSecret(): void {
  kept = null;
  try {
    localStorage.removeItem(key);
  } catch {
    // nothing kept
  }
}
