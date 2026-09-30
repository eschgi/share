import { describe, expect, it } from 'vitest';
import type { Session } from '../src/api';
import { initialState, reduce, type Action, type State } from '../src/state';

const session: Session = { kind: 'pin', pin_kind: 'permanent', expires_at: null };
const run = (...actions: Action[]): State => actions.reduce(reduce, initialState);

describe('screen flow', () => {
  it('goes to the PIN screen without a session, and to ready with one', () => {
    expect(run({ type: 'booted', session: null, sessionEnded: false }).screen).toBe('pin');
    expect(run({ type: 'booted', session, sessionEnded: false }).screen).toBe('ready');
  });

  it('says why when the stored session has ended', () => {
    const s = run({ type: 'booted', session: null, sessionEnded: true });
    expect(s.problem).toEqual({ kind: 'sessionEnded' });
  });

  it('unlocks, sends, finishes and starts over', () => {
    let s = run({ type: 'booted', session: null, sessionEnded: false }, { type: 'unlockStarted' });
    expect(s.unlocking).toBe(true);
    s = reduce(s, { type: 'unlocked', session });
    expect(s.screen).toBe('ready');
    s = reduce(s, { type: 'filesAdded' });
    expect(s.screen).toBe('sending');
    s = reduce(s, { type: 'allDone', files: 3, bytes: 1000 });
    expect(s).toMatchObject({ screen: 'done', done: { files: 3, bytes: 1000 } });
    s = reduce(s, { type: 'sendMore' });
    expect(s).toMatchObject({ screen: 'ready', done: null });
  });

  it('keeps the PIN screen after a wrong try', () => {
    const s = run(
      { type: 'booted', session: null, sessionEnded: false },
      { type: 'unlockStarted' },
      { type: 'unlockFailed', problem: { kind: 'wrong', attemptsLeft: 4 } },
    );
    expect(s).toMatchObject({ screen: 'pin', unlocking: false, problem: { kind: 'wrong', attemptsLeft: 4 } });
  });

  it('returns to sending after a new PIN when the old one ended mid-upload', () => {
    let s = run({ type: 'booted', session, sessionEnded: false }, { type: 'filesAdded' }, { type: 'sessionEnded' });
    expect(s).toMatchObject({ screen: 'pin', waitingForPin: true, problem: { kind: 'sessionEnded' } });
    // Uploads can't finish without a session, so "all done" must not jump ahead.
    expect(reduce(s, { type: 'allDone', files: 1, bytes: 1 }).screen).toBe('pin');
    s = reduce(s, { type: 'unlocked', session });
    expect(s).toMatchObject({ screen: 'sending', waitingForPin: false });
  });
});

describe('an upload that came back after the page closed', () => {
  it('shows screen 5, whether the restore or the session check finishes first', () => {
    expect(run({ type: 'restored' }, { type: 'booted', session, sessionEnded: false }).screen).toBe('welcome');
    expect(run({ type: 'booted', session, sessionEnded: false }, { type: 'restored' }).screen).toBe('welcome');
  });

  it('asks for a new PIN first when the old one has ended', () => {
    let s = run({ type: 'restored' }, { type: 'booted', session: null, sessionEnded: true });
    expect(s).toMatchObject({ screen: 'pin', problem: { kind: 'sessionEnded' } });
    s = reduce(s, { type: 'unlocked', session });
    expect(s.screen).toBe('welcome');
  });

  it('continues on the sending screen, or starts over on the ready screen', () => {
    const s = run({ type: 'booted', session, sessionEnded: false }, { type: 'restored' });
    expect(reduce(s, { type: 'continued' })).toMatchObject({ screen: 'sending', restored: false });
    expect(reduce(s, { type: 'startedOver' })).toMatchObject({ screen: 'ready', restored: false });
  });

  it('stays on screen 5 while files are picked again there', () => {
    const s = run({ type: 'booted', session, sessionEnded: false }, { type: 'restored' }, { type: 'filesAdded' });
    expect(s.screen).toBe('welcome');
  });

  it('changes nothing once sending runs', () => {
    const s = run({ type: 'booted', session, sessionEnded: false }, { type: 'filesAdded' }, { type: 'restored' });
    expect(s).toMatchObject({ screen: 'sending', restored: false });
  });
});
