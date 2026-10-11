import { describe, expect, it } from 'vitest';
import type { AppInfo } from '../src/api';
import { accessFromHash, intentLink, secretFromHash, tokenFromHash } from '../src/joinlink';

const token = 'shi_' + 'a'.repeat(32);
const secret = 'B'.repeat(42) + 'w';
const access = '0123456789abcdef0123456789abcdef.access:' + 'f'.repeat(64);
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

  it('reads the Cloudflare Access token after the &, and keeps it out of the token and the secret', () => {
    expect(tokenFromHash(`#${token}.${secret}&cfa=${access}`)).toBe(token);
    expect(secretFromHash(`#${token}.${secret}&cfa=${access}`)).toBe(secret);
    expect(tokenFromHash(`#${token}&cfa=${access}`)).toBe(token);
    expect(accessFromHash(`#${token}.${secret}&cfa=${access}`)).toBe(access);
    expect(accessFromHash(`#${token}&cfa=${access}`)).toBe(access);
    expect(accessFromHash(`#${token}.${secret}`)).toBeNull();
    expect(accessFromHash(`#${token}&cfa=nodot:${'f'.repeat(64)}`)).toBeNull();
    expect(accessFromHash(`#${token}&cfa=abc.access:short`)).toBeNull();
  });

  it('hands the Cloudflare Access token on to the app, and back to this page', () => {
    const link = intentLink(app, 'https://share.example.com', token, secret, access);
    const q = new URLSearchParams(link.slice('intent://join?'.length, link.indexOf('#')));
    expect(q.get('cfa')).toBe(access);
    expect(decodeURIComponent(/S\.browser_fallback_url=([^;]*)/.exec(link)![1])).toBe(`https://share.example.com/join#${token}.${secret}&cfa=${access}`);
    expect(intentLink(app, 'https://share.example.com', token, secret)).not.toContain('cfa=');
  });
});
