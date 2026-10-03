// What a guest's look into a folder works out without a screen.

/** What the server will call the ZIP of everything: its name and the day the files came, or
 * today if they came on several days. */
export function guestZipName(name: string, days: string[], now: Date): string {
  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
  return `${name} ${days.length === 1 ? days[0] : today}.zip`;
}
