// What an invite link carries: <server>/join#shi_…, maybe followed by .<secret>, and in an
// admin's QR code &cfa=<client id>:<client secret> after that; and how the link goes on to the
// installed app.
import type { AppInfo } from './api';

/** The token and its secret, before the &, and what follows it. */
function parts(hash: string): { head: string; params: URLSearchParams } {
  const [head, ...rest] = decodeURIComponent(hash.replace(/^#/, '')).split('&');
  return { head, params: new URLSearchParams(rest.join('&')) };
}

/** The invite token from the link: <server>/join#shi_…, maybe followed by .<secret>. */
export function tokenFromHash(hash: string): string | null {
  const t = parts(hash).head.split('.')[0];
  return /^shi_[A-Za-z0-9_-]{20,}$/.test(t) ? t : null;
}

/** The secret after the invite token's dot, which unlocks the keys the inviting device locked
 * with it (docs/e2ee-plan.md); it never goes to the server. */
export function secretFromHash(hash: string): string | null {
  const s = parts(hash).head.split('.')[1] ?? '';
  return /^[A-Za-z0-9_-]{43}$/.test(s) ? s : null;
}

/** The Cloudflare Access service token an admin's phone put into the QR code, as
 * <client id>:<client secret>, for the app (docs/cloudflare-access-plan.md); it never goes to
 * the server. */
export function accessFromHash(hash: string): string | null {
  const a = parts(hash).params.get('cfa') ?? '';
  return /^[A-Za-z0-9-]+\.access:[A-Za-z0-9]{16,}$/.test(a) ? a : null;
}

/**
 * The link that hands the invite to the installed app. Chrome opens the app with
 * <scheme>://join?server=…&token=…(&key=…)(&cfa=…), or, without the app, comes back to this page.
 */
export function intentLink(app: AppInfo, server: string, token: string, secret: string | null = null, access: string | null = null): string {
  const q = new URLSearchParams({ server, token, ...(secret ? { key: secret } : {}), ...(access ? { cfa: access } : {}) }).toString();
  const fallback = encodeURIComponent(`${server}/join#${token}${secret ? '.' + secret : ''}${access ? '&cfa=' + access : ''}`);
  return `intent://join?${q}#Intent;scheme=${app.link_scheme};package=${app.android_package};S.browser_fallback_url=${fallback};end`;
}
