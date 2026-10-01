import { describe, expect, it } from 'vitest';
import { qrCode, qrPath, quietZone } from '../src/qr';

describe('qrPath', () => {
  it('draws a rectangle per run of dark modules, inside the quiet zone', () => {
    const dark = new Set(['0,0', '1,0', '2,1']);
    const code = { size: 3, get: (x: number, y: number) => dark.has(`${x},${y}`) };
    expect(qrPath(code)).toBe(`M${quietZone} ${quietZone}h2v1h-2zM${quietZone + 2} ${quietZone + 1}h1v1h-1z`);
  });
});

describe('qrCode', () => {
  const link = 'https://share.eschgi.com/join#shi_F6mJlwwvMiBboD5wHufbdpckGbsmyPxEdAdpZpWvmT0';

  it('encodes an invite link in a size a phone camera reads easily', () => {
    const code = qrCode(link);
    expect((code.size - 17) % 4).toBe(0); // a valid QR version
    expect(code.size).toBeLessThanOrEqual(45);
  });

  it('has the three finder patterns in their corners', () => {
    const code = qrCode(link);
    const finder = (x0: number, y0: number) => {
      const ring = (d: number) => [0, 1, 2, 3, 4, 5, 6].every((i) => code.get(x0 + i, y0 + d) && code.get(x0 + d, y0 + i));
      return ring(0) && ring(6) && !code.get(x0 + 1, y0 + 1) && code.get(x0 + 3, y0 + 3);
    };
    expect(finder(0, 0)).toBe(true);
    expect(finder(code.size - 7, 0)).toBe(true);
    expect(finder(0, code.size - 7)).toBe(true);
  });
});
