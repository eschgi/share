// Whether this browser was signed in last time. Its pages can then start loading at once, and
// open without a connection. Only a hint: the server decides.

const key = 'share.signedIn';

export function setSignedInHint(on: boolean): void {
  try {
    if (on) localStorage.setItem(key, '1');
    else localStorage.removeItem(key);
  } catch {
    // without storage there's just no head start
  }
}

export function signedInHint(): boolean {
  try {
    return localStorage.getItem(key) === '1';
  } catch {
    return false;
  }
}
