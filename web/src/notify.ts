// Notifications when sending or saving ends while the page is in the background, for big
// transfers that are left running. The browser asks for the right once, after a tap on "Tell me
// when it's done" (browsers only ask after a tap). Nothing shows while the page is in front, and
// nothing over plain http, where browsers have no notifications.
import { formatBytes } from './format';
import { translate, translatePlural, type Lang } from './i18n';
import { noticeText, type Notice } from './notices';

/** From this much on, a transfer offers to tell when it's done. */
export const bigTransfer = 200 * 1024 * 1024;

let lang: Lang = 'en';

/** The language the notifications speak: the page's. */
export function noticeLanguage(l: Lang): void {
  lang = l;
}

/** Whether the page could notify at all. */
export function canNotify(): boolean {
  return isSecureContext && typeof Notification !== 'undefined';
}

/** The browser hasn't been asked yet: "Tell me when it's done" can ask. */
export function mayAskToNotify(): boolean {
  return canNotify() && Notification.permission === 'default';
}

/** In a tap: asks for the right to notify; true if given. */
export async function askToNotify(): Promise<boolean> {
  if (!canNotify()) return false;
  try {
    return (await Notification.requestPermission()) === 'granted';
  } catch {
    return false;
  }
}

/** Says what ended, if the page is in the background and may notify. */
export async function notify(n: Notice): Promise<void> {
  if (!canNotify() || Notification.permission !== 'granted' || document.visibilityState === 'visible') return;
  const { title, body, tag } = noticeText(n, {
    t: (key, params) => translate(lang, key, params),
    tn: (key, count, params) => translatePlural(lang, key, count, params),
    bytes: (b) => formatBytes(b, lang),
  });
  // renotify: a newer notice in place of an older one rings again.
  const options = { body, tag: `share-${tag}`, renotify: true, icon: '/icons/icon-192.png', data: { url: location.pathname } };
  try {
    const reg = await navigator.serviceWorker?.getRegistration();
    if (reg) await reg.showNotification(title, options);
    else new Notification(title, options);
  } catch {
    // Not shown, then: the page says it when it's back in front.
  }
}
