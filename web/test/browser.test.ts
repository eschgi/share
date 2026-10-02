import { describe, expect, it } from 'vitest';
import { browserName, isApple } from '../src/browser';

const ua = {
  chromeWindows: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36',
  edgeWindows:
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0',
  operaWindows:
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 OPR/124.0.0.0',
  firefoxLinux: 'Mozilla/5.0 (X11; Linux x86_64; rv:143.0) Gecko/20100101 Firefox/143.0',
  safariMac: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Safari/605.1.15',
  safariIphone:
    'Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Mobile/15E148 Safari/604.1',
  chromeIphone:
    'Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/141.0.7390.41 Mobile/15E148 Safari/604.1',
  firefoxIpad:
    'Mozilla/5.0 (iPad; CPU OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/143.0 Mobile/15E148 Safari/605.1.15',
  chromeAndroid: 'Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36',
  chromeAndroidTablet: 'Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36',
  samsungAndroid:
    'Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/28.0 Chrome/130.0.0.0 Mobile Safari/537.36',
  firefoxAndroid: 'Mozilla/5.0 (Android 15; Mobile; rv:143.0) Gecko/143.0 Firefox/143.0',
  chromebook: 'Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36',
};

describe('browserName', () => {
  it.each([
    [ua.chromeWindows, 'Chrome · Windows'],
    [ua.edgeWindows, 'Edge · Windows'],
    [ua.operaWindows, 'Opera · Windows'],
    [ua.firefoxLinux, 'Firefox · Linux'],
    [ua.safariMac, 'Safari · Mac'],
    [ua.safariIphone, 'Safari · iPhone'],
    [ua.chromeIphone, 'Chrome · iPhone'],
    [ua.firefoxIpad, 'Firefox · iPad'],
    [ua.chromeAndroid, 'Chrome · Android'],
    [ua.chromeAndroidTablet, 'Chrome · Android tablet'],
    [ua.samsungAndroid, 'Samsung Internet · Android'],
    [ua.firefoxAndroid, 'Firefox · Android'],
    [ua.chromebook, 'Chrome · ChromeOS'],
    ['curl/8.5.0', 'Browser · Computer'],
  ])('names %s', (agent, name) => {
    expect(browserName(agent)).toBe(name);
  });

  it('prefers what Client Hints say', () => {
    const brands = (b: string) => [{ brand: 'Not)A;Brand' }, { brand: b }, { brand: 'Chromium' }];
    expect(browserName(ua.chromeWindows, { brands: brands('Brave'), platform: 'Windows' })).toBe('Brave · Windows');
    expect(browserName(ua.chromeWindows, { brands: brands('Microsoft Edge'), platform: 'macOS' })).toBe('Edge · Mac');
    expect(browserName(ua.chromeWindows, { brands: brands('Google Chrome'), platform: 'Chrome OS' })).toBe('Chrome · ChromeOS');
    expect(browserName(ua.chromeAndroid, { brands: brands('Google Chrome'), platform: 'Android' })).toBe('Chrome · Android');
  });
});

describe('isApple', () => {
  it('knows iPhones and iPads, also when an iPad says it is a Mac', () => {
    expect(isApple(ua.safariIphone, 5)).toBe(true);
    expect(isApple(ua.firefoxIpad, 5)).toBe(true);
    expect(isApple(ua.safariMac, 5)).toBe(true);
    expect(isApple(ua.safariMac, 0)).toBe(false);
    expect(isApple(ua.chromeAndroid, 5)).toBe(false);
    expect(isApple(ua.chromeWindows, 10)).toBe(false);
  });
});
