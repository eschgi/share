import { afterEach, describe, expect, it, vi } from 'vitest';
import setup from '../../contract/api/setup.json';
import invite from '../../contract/api/setup_invite.json';
import start from '../../contract/api/setup_start.json';
import { ApiError, getSetup, setupInvite, startSetup } from '../src/api';
import { secretFromHash, setupProblem, stillStarting } from '../src/setupflow';

describe('secretFromHash', () => {
  it('takes the secret of a setup link', () => {
    expect(secretFromHash('#q3Jx0e5mJ1pWZ8yQd2vB7nK4tR6sA9cF')).toBe('q3Jx0e5mJ1pWZ8yQd2vB7nK4tR6sA9cF');
  });
  it('leaves links without one', () => {
    expect(secretFromHash('')).toBeNull();
    expect(secretFromHash('#')).toBeNull();
    expect(secretFromHash('#q3Jx0e5m')).toBeNull();
    expect(secretFromHash('#q3Jx0e5mJ1pWZ8yQd2vB7nK4tR6sA9cF%20')).toBeNull();
  });
});

describe('setupProblem', () => {
  it('says which link to open', () => {
    const forbidden = new ApiError(403, 'forbidden', 'This setup link is from an earlier start.');
    expect(setupProblem(forbidden, true)).toEqual({ kind: 'link', key: 'setup.earlier' });
    expect(setupProblem(forbidden, false)).toEqual({ kind: 'link', key: 'setup.noSecret' });
  });
  it('takes a server without a setup, or with an admin, as set up', () => {
    expect(setupProblem(new ApiError(404, 'not_found', 'No such thing.'), true)).toEqual({ kind: 'done' });
    expect(setupProblem(new ApiError(409, 'already_set_up', 'Share has an admin already.'), true)).toEqual({ kind: 'done' });
  });
  it("gives the server's words, or none when it can't be reached", () => {
    expect(setupProblem(new ApiError(403, 'proxy_untrusted', 'A proxy config.json doesn’t name.'), true)).toEqual({
      kind: 'failed',
      reason: 'A proxy config.json doesn’t name.',
    });
    expect(setupProblem(new ApiError(0, 'network', 'The server can’t be reached.'), true)).toEqual({ kind: 'failed', reason: null });
  });
});

describe('stillStarting', () => {
  it('waits while nothing answers, or a proxy says the server is away', () => {
    expect(stillStarting(new ApiError(0, 'network', ''))).toBe(true);
    expect(stillStarting(new ApiError(502, 'internal', 'Bad Gateway'))).toBe(true);
    expect(stillStarting(new TypeError('Failed to fetch'))).toBe(true);
  });
  it('stops at an answer', () => {
    expect(stillStarting(new ApiError(403, 'forbidden', ''))).toBe(false);
    expect(stillStarting(new ApiError(404, 'not_found', ''))).toBe(false);
  });
});

describe('the setup API', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('sends the secret in its header, as the contract has it', async () => {
    const calls: [string, RequestInit][] = [];
    vi.stubGlobal('fetch', async (path: string, init: RequestInit) => {
      calls.push([path, init]);
      if (path === invite.path) return Response.json(invite.response, { status: invite.status });
      if (init.method === 'GET') return Response.json(setup.response);
      return new Response(null, { status: start.status });
    });
    expect(await getSetup('s3cret')).toEqual(setup.response);
    await startSetup('s3cret');
    expect(await setupInvite('s3cret')).toEqual(invite.response);
    expect(calls.map(([path, init]) => `${init.method} ${path}`)).toEqual([
      `${setup.method} ${setup.path}`,
      `${start.method} ${start.path}`,
      `${invite.method} ${invite.path}`,
    ]);
    for (const [, init] of calls) expect(init.headers).toMatchObject({ 'X-Share-Setup': 's3cret' });
    expect(JSON.parse(calls[1][1].body as string)).toEqual(start.request);
    expect(calls[1][1].headers).toMatchObject({ 'Content-Type': 'application/json' });
  });
});
