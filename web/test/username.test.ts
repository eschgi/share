import { describe, expect, it } from 'vitest';
import usernames from '../../contract/usernames.json';
import { suggestedUsername, usernamePattern, validUsername } from '../src/account/settings/username';

describe('usernames', () => {
  it.each(usernames.valid)('takes %s, as the server does', (u) => {
    expect(validUsername(u)).toBe(true);
  });
  it.each(usernames.invalid)('refuses "%s", as the server does', (u) => {
    expect(validUsername(u)).toBe(false);
  });
  it.each(usernames.suggestions)('suggests "$username" for "$name", as the app does', ({ name, username }) => {
    expect(suggestedUsername(name)).toBe(username);
  });
  it("has the server's pattern", () => {
    expect(usernamePattern.source).toBe(usernames.pattern);
  });
});
