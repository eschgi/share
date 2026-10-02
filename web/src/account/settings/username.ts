// Usernames as the server takes them, and the one suggested for someone without one, as the app
// suggests it too (contract/usernames.json).

export const usernamePattern = /^[\p{L}\p{N}._-]{3,32}$/u;

/** Whether the server takes it: 3 to 32 letters, digits, dots, dashes or underscores. */
export function validUsername(username: string): boolean {
  return usernamePattern.test(username.trim());
}

/** The name in small letters, with dots for spaces and only what a username may have; empty if
 * that leaves too little. */
export function suggestedUsername(name: string): string {
  const trim = (s: string) => s.replace(/^[._-]+|[._-]+$/g, '');
  const s = trim(
    name
      .trim()
      .toLowerCase()
      .replace(/\s+/gu, '.')
      .replace(/[^\p{L}\p{N}._-]/gu, '')
      .replace(/\.{2,}/g, '.'),
  );
  const cut = trim([...s].slice(0, 32).join(''));
  return validUsername(cut) ? cut : '';
}
