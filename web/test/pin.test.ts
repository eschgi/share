import { describe, expect, it } from 'vitest';
import contract from '../../contract/pin_codes.json';
import { cleanPinInput, normalizePin, pinAlphabet, pinFromHash, pinLength, pinSecretFromHash } from '../src/pin';

describe('PIN rules', () => {
  it('uses the same alphabet and length as the server', () => {
    expect(pinAlphabet).toBe(contract.alphabet);
    expect(pinLength).toBe(contract.length);
  });

  it.each(contract.cases)('normalizes %j like the server', ({ input, code }) => {
    expect(normalizePin(input)).toBe(code);
  });

  it('keeps only usable characters while typing', () => {
    expect(cleanPinInput('k7-m 2q9')).toBe('K7M2Q');
    expect(cleanPinInput('o0i1')).toBe('');
  });

  it('reads a PIN from the link', () => {
    expect(pinFromHash('#k7m2q')).toBe('K7M2Q');
    expect(pinFromHash('#nope')).toBeNull();
    expect(pinFromHash('')).toBeNull();
  });

  it('reads the secret of a PIN link that shows an encrypted folder', () => {
    const secret = 'x'.repeat(42) + 'A';
    expect(pinFromHash(`#K7M2Q.${secret}`)).toBe('K7M2Q');
    expect(pinSecretFromHash(`#K7M2Q.${secret}`)).toBe(secret);
    expect(pinSecretFromHash('#K7M2Q')).toBeNull();
    expect(pinSecretFromHash(`#K7M2Q.${secret.slice(2)}`)).toBeNull();
    expect(pinSecretFromHash(`#.${secret}`)).toBeNull();
  });
});
