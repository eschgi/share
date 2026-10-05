// What an invite link carries: <server>/join#shi_…, maybe followed by .<secret>, and how the
// link goes on to the installed app.
import type { AppInfo } from './api';

/** The invite token from the link: <server>/join#shi_…, maybe followed by .<secret>. */
export function tokenFromHash(hash: string): string | null {
  const t = decodeURIComponent(hash.replace(/^#/, '')).split('.')[0];
  return /^shi_[A-Za-z0-9_-]{20,}$/.test(t) ? t : null;
}

/** The secret after the invite token's dot, which unlocks the keys the inviting device locked
 * with it (docs/e2ee-plan.md); it never goes to the server. */
export function secretFromHash(hash: string): string | null {
  const s = decodeURIComponent(hash.replace(/^#/, '')).split('.')[1] ?? '';
  return /^[A-Za-z0-9_-]{43}$/.test(s) ? s : null;
}

/**
 * The link that hands the invite to the installed app. Chrome opens the app with
 * <scheme>://join?server=…&token=…(&key=…), or, without the app, comes back to this page.
 */
export function intentLink(app: AppInfo, server: string, token: string, secret: string | null = null): string {
  const q = new URLSearchParams({ server, token, ...(secret ? { key: secret } : {}) }).toString();
  const fallback = encodeURIComponent(`${server}/join#${token}${secret ? '.' + secret : ''}`);
  return `intent://join?${q}#Intent;scheme=${app.link_scheme};package=${app.android_package};S.browser_fallback_url=${fallback};end`;
}
