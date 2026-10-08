import { describe, expect, it } from 'vitest';
import contract from '../../contract/pin_codes.json';
import { cleanPinInput, normalizePin, pinAlphabet, pinFromHash, pinLength, pinRootFromHash, pinSecretFromHash, typedInto } from '../src/pin';

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
    expect(cleanPinInput('anna-1')).toBe('ANNA1');
    expect(cleanPinInput('ä é!')).toBe('');
  });

  it('takes what is typed into a made-up code, wherever the cursor was', () => {
    expect(typedInto('R8D4W', 'R8D4WA')).toBe('A');
    expect(typedInto('R8D4W', 'AR8D4W')).toBe('A');
    expect(typedInto('R8D4W', 'R8AD4W')).toBe('A');
    expect(typedInto('R8D4W', 'R8D4WANNA1')).toBe('ANNA1');
    expect(typedInto('AAAAA', 'AAAAAA')).toBe('A');
    // A deletion or a replacement stays as it is.
    expect(typedInto('R8D4W', 'R8D4')).toBe('R8D4');
    expect(typedInto('R8D4W', 'K')).toBe('K');
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

  it("reads the root's fingerprint of a PIN link that only sends into a folder with keys", () => {
    const fp = 'y'.repeat(21) + 'Q';
    expect(pinFromHash(`#K7M2Q.${fp}`)).toBe('K7M2Q');
    expect(pinRootFromHash(`#K7M2Q.${fp}`)).toBe(fp);
    expect(pinSecretFromHash(`#K7M2Q.${fp}`)).toBeNull();
    expect(pinRootFromHash(`#K7M2Q.${'x'.repeat(43)}`)).toBeNull();
    expect(pinRootFromHash('#K7M2Q')).toBeNull();
  });
});
