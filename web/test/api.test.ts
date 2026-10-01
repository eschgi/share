import { describe, expect, it } from 'vitest';
import { errorCode } from '../src/api';

describe('errorCode', () => {
  it('reads the code of an error body', () => {
    expect(errorCode('{"error":{"code":"session_ended","message":"…"}}')).toBe('session_ended');
    expect(errorCode('{"error":{"code":"unauthorized"}}')).toBe('unauthorized');
  });

  it('gives nothing for bodies without one', () => {
    expect(errorCode(undefined)).toBeUndefined();
    expect(errorCode('')).toBeUndefined();
    expect(errorCode('<html>Bad gateway</html>')).toBeUndefined();
    expect(errorCode('null')).toBeUndefined();
    expect(errorCode('{"error":{"code":7}}')).toBeUndefined();
  });
});
