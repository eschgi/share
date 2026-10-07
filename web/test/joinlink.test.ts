import { describe, expect, it } from 'vitest';
import type { AppInfo } from '../src/api';
import { intentLink, secretFromHash, tokenFromHash } from '../src/joinlink';

const token = 'shi_' + 'a'.repeat(32);
const secret = 'B'.repeat(42) + 'w';
const app = { link_scheme: 'share', android_package: 'com.eschgi.share' } as AppInfo;

describe('invite links', () => {
  it('reads the token, with or without the secret', () => {
    expect(tokenFromHash('#' + token)).toBe(token);
    expect(tokenFromHash(`#${token}.${secret}`)).toBe(token);
    expect(tokenFromHash('#shi_short')).toBeNull();
    expect(tokenFromHash('')).toBeNull();
  });

  it('reads the secret only when it is 32 bytes in base64url', () => {
    expect(secretFromHash(`#${token}.${secret}`)).toBe(secret);
    expect(secretFromHash('#' + token)).toBeNull();
    expect(secretFromHash(`#${token}.${secret.slice(1)}`)).toBeNull();
    expect(secretFromHash(`#${token}.${secret}=`)).toBeNull();
  });

  it('hands the secret on to the app, and back to this page without it', () => {
    const link = intentLink(app, 'https://share.example.com', token, secret);
    const q = new URLSearchParams(link.slice('intent://join?'.length, link.indexOf('#')));
    expect(q.get('token')).toBe(token);
    expect(q.get('key')).toBe(secret);
    expect(decodeURIComponent(/S\.browser_fallback_url=([^;]*)/.exec(link)![1])).toBe(`https://share.example.com/join#${token}.${secret}`);
    expect(intentLink(app, 'https://share.example.com', token)).not.toContain('key=');
  });
});
