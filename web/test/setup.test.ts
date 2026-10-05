import { afterEach, describe, expect, it, vi } from 'vitest';
import admin from '../../contract/api/setup_admin.json';
import setup from '../../contract/api/setup.json';
import ready from '../../contract/api/setup_ready.json';
import start from '../../contract/api/setup_start.json';
import { ApiError, createFirstAdmin, getSetup, startSetup, type SetupStatus } from '../src/api';
import { secretFromHash, setupProblem, setupStep, stillStarting } from '../src/setupflow';

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

describe('setupStep', () => {
  it('sets the folder up, then makes the first admin', () => {
    const folder = setup.response as SetupStatus;
    expect(setupStep(folder)).toEqual({ kind: 'folder', status: folder });
    expect(setupStep(ready.response as SetupStatus)).toEqual({ kind: 'admin' });
    expect(setupStep({ ...(ready.response as SetupStatus), needs_admin: false })).toEqual({ kind: 'done' });
  });
});

describe('setupProblem', () => {
  it('says when only home, a restart or the link in the log lets one in', () => {
    expect(setupProblem(new ApiError(403, 'setup_closed', '…'))).toEqual({ kind: 'closed' });
  });
  it('takes a server with an admin as set up', () => {
    expect(setupProblem(new ApiError(409, 'already_set_up', 'Share has an admin already.'))).toEqual({ kind: 'done' });
  });
  it("gives the server's words, or none when it can't be reached", () => {
    expect(setupProblem(new ApiError(403, 'proxy_untrusted', 'A proxy config.json doesn’t name.'))).toEqual({
      kind: 'failed',
      reason: 'A proxy config.json doesn’t name.',
    });
    expect(setupProblem(new ApiError(0, 'network', 'The server can’t be reached.'))).toEqual({ kind: 'failed', reason: null });
  });
});

describe('stillStarting', () => {
  it('waits while nothing answers, or a proxy says the server is away', () => {
    expect(stillStarting(new ApiError(0, 'network', ''))).toBe(true);
    expect(stillStarting(new ApiError(502, 'internal', 'Bad Gateway'))).toBe(true);
    expect(stillStarting(new TypeError('Failed to fetch'))).toBe(true);
  });
  it('stops at an answer', () => {
    expect(stillStarting(new ApiError(403, 'setup_closed', ''))).toBe(false);
    expect(stillStarting(new ApiError(404, 'not_found', ''))).toBe(false);
  });
});

describe('the setup API', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('asks as the contract has it, with the secret of the link when there is one', async () => {
    const calls: [string, RequestInit][] = [];
    vi.stubGlobal('fetch', async (path: string, init: RequestInit) => {
      calls.push([path, init]);
      if (init.method === 'GET') return Response.json(setup.response);
      return new Response(null, { status: 204 });
    });
    expect(await getSetup(null)).toEqual(setup.response);
    await startSetup('s3cret');
    await createFirstAdmin(null, admin.request);
    expect(calls.map(([path, init]) => `${init.method} ${path}`)).toEqual([
      `${setup.method} ${setup.path}`,
      `${start.method} ${start.path}`,
      `${admin.method} ${admin.path}`,
    ]);
    expect(calls[0][1].headers).not.toHaveProperty('X-Share-Setup');
    expect(calls[1][1].headers).toMatchObject({ 'X-Share-Setup': 's3cret', 'Content-Type': 'application/json' });
    expect(JSON.parse(calls[1][1].body as string)).toEqual(start.request);
    expect(JSON.parse(calls[2][1].body as string)).toEqual(admin.request);
  });
});
