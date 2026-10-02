// The storage row's warnings: what share check finds about the drive, in the admin's words.
import type { StorageWarning } from '../../api';

/** The text's key: every code of contract/storage_warnings.json has one; a code from a newer
 * server than this page gets a general line. has tells whether a key is translated. */
export function warningKey(code: string, has: (key: string) => boolean): string {
  const key = `storage.warn.${code}`;
  return has(key) ? key : 'storage.warn.unknown';
}

/** Whether any of them keeps Share from working well. */
export function hasProblem(warnings: StorageWarning[] | undefined): boolean {
  return (warnings ?? []).some((w) => w.level === 'problem');
}
