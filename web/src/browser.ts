// A name for this browser, such as "Chrome · Windows", for the list of a person's phones and
// browsers. It names no language, so it reads the same to everyone.

interface Brand {
  brand: string;
}

/** The browser and system from the user agent (and Client Hints, where the browser has them). */
export function browserName(ua: string, hints?: { brands?: Brand[]; platform?: string }): string {
  return `${browserOf(ua, hints?.brands)} · ${systemOf(ua, hints?.platform)}`;
}

function browserOf(ua: string, brands?: Brand[]): string {
  const named = brands?.map((b) => b.brand).find((b) => /^(Microsoft Edge|Google Chrome|Opera|Brave|Vivaldi|Samsung Internet)$/.test(b));
  if (named) return named.replace(/^Google /, '').replace(/^Microsoft /, '');
  if (/Edg(e|A|iOS)?\//.test(ua)) return 'Edge';
  if (/OPR\/|Opera/.test(ua)) return 'Opera';
  if (/SamsungBrowser\//.test(ua)) return 'Samsung Internet';
  if (/Firefox\/|FxiOS\//.test(ua)) return 'Firefox';
  if (/Chrome\/|CriOS\//.test(ua)) return 'Chrome';
  if (/Safari\//.test(ua)) return 'Safari';
  return 'Browser';
}

function systemOf(ua: string, platform?: string): string {
  if (/iPhone/.test(ua)) return 'iPhone';
  if (/iPad/.test(ua)) return 'iPad';
  if (/Android/.test(ua)) return /Mobile/.test(ua) ? 'Android' : 'Android tablet';
  switch (platform) {
    case 'Windows':
      return 'Windows';
    case 'macOS':
      return 'Mac';
    case 'Chrome OS':
    case 'ChromeOS':
      return 'ChromeOS';
    case 'Linux':
      return 'Linux';
  }
  if (/Windows/.test(ua)) return 'Windows';
  if (/Macintosh|Mac OS X/.test(ua)) return 'Mac';
  if (/CrOS/.test(ua)) return 'ChromeOS';
  if (/Linux/.test(ua)) return 'Linux';
  return 'Computer';
}

/** iPhones and iPads, for which there is no app. iPads say they are Macs; their touch screen
 * tells them apart. */
export function isApple(ua: string, touchPoints: number): boolean {
  return /iPhone|iPad|iPod/.test(ua) || (/Macintosh/.test(ua) && touchPoints > 1);
}

/** This browser's name. */
export function thisBrowser(): string {
  const hints = (navigator as Navigator & { userAgentData?: { brands?: Brand[]; platform?: string } }).userAgentData;
  let ua = navigator.userAgent;
  if (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1) ua = ua.replace('Macintosh', 'iPad');
  return browserName(ua, hints);
}
