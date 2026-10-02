import { describe, expect, it } from 'vitest';
import routes from '../../contract/web_routes.json';
import { decide, isAppPath, nextAfterSignIn } from '../src/paths';

describe('isAppPath', () => {
  it.each(routes.examples.page)('takes %s, as the server does', (p) => {
    expect(isAppPath(p)).toBe(true);
  });
  it.each(routes.examples.not_found)('leaves %s, as the server does', (p) => {
    expect(isAppPath(p)).toBe(false);
  });
  it('leaves the invite page and files', () => {
    expect(isAppPath('/join')).toBe(false);
    expect(isAppPath('/sw.js')).toBe(false);
    expect(isAppPath('/assets/index-3f2a.js')).toBe(false);
  });
});

describe('decide', () => {
  it('takes people with an account to their pages', () => {
    expect(decide('/', true, false)).toEqual({ view: 'account', redirect: '/library' });
    expect(decide('/', true, true)).toEqual({ view: 'account', redirect: '/send' });
    expect(decide('/sign-in', true, false)).toEqual({ view: 'account', redirect: '/library' });
    expect(decide('/send', true, false)).toEqual({ view: 'account' });
    expect(decide('/settings/people', true, false)).toEqual({ view: 'account' });
  });
  it('shows everyone else the PIN, or the sign-in first', () => {
    expect(decide('/', false, false)).toEqual({ view: 'pin' });
    expect(decide('/', false, true)).toEqual({ view: 'pin' });
    expect(decide('/sign-in', false, false)).toEqual({ view: 'signIn' });
    expect(decide('/send', false, false)).toEqual({ view: 'pin', redirect: '/' });
    expect(decide('/library', false, false)).toEqual({ view: 'signIn', redirect: '/sign-in?next=%2Flibrary' });
    expect(decide('/settings/people', false, false)).toEqual({ view: 'signIn', redirect: '/sign-in?next=%2Fsettings%2Fpeople' });
  });
});

describe('nextAfterSignIn', () => {
  it('goes back to the page asked for', () => {
    expect(nextAfterSignIn('?next=%2Fsettings%2Fpeople')).toBe('/settings/people');
    expect(nextAfterSignIn('?next=/send')).toBe('/send');
  });
  it('goes to the library otherwise, and never elsewhere', () => {
    expect(nextAfterSignIn('')).toBe('/library');
    expect(nextAfterSignIn('?next=%2F')).toBe('/library');
    expect(nextAfterSignIn('?next=%2Fsign-in')).toBe('/library');
    expect(nextAfterSignIn('?next=%2Fjoin')).toBe('/library');
    expect(nextAfterSignIn('?next=https%3A%2F%2Fevil.example%2Flibrary')).toBe('/library');
    expect(nextAfterSignIn('?next=%2F%2Fevil.example%2Flibrary')).toBe('/library');
    expect(nextAfterSignIn('?next=%2F%5Cevil.example')).toBe('/library');
  });
});
