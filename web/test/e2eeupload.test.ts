import { describe, expect, it } from 'vitest';
import { concat, fromB64u } from '../src/e2ee/bytes';
import { ContentCipher, decryptFile, encryptedSize } from '../src/e2ee/content';
import { generateKeyPair } from '../src/e2ee/formats';
import { encryptingReader, newSeal, remember, sealOf } from '../src/e2ee/upload';

describe('encrypted uploads', () => {
  it('give tus Blobs of the encrypted stream, which decrypt to the file', async () => {
    const pair = await generateKeyPair(false);
    const data = new Uint8Array(200_000).map((_, i) => (i * 7) & 0xff);
    const file = new Blob([data]);
    const seal = await newSeal({ folder: 'f', version: 1, publicKey: pair.publicKey }, file.size, '1');
    remember(file, seal);
    expect(sealOf(file)).toBe(seal);
    const source = await encryptingReader.openFile(file);
    expect(source.size).toBe(encryptedSize(file.size));
    const parts: Uint8Array[] = [];
    for (let at = 0; ; ) {
      const { value, done } = await source.slice(at, at + 50_000);
      // tus counts what it sent by the slice's size.
      expect(value).toBeInstanceOf(Blob);
      parts.push(new Uint8Array(await value.arrayBuffer()));
      at += value.size;
      if (done) break;
    }
    const cipher = await ContentCipher.create(fromB64u(seal.key), fromB64u(seal.header), file.size);
    expect(await decryptFile(cipher, concat(...parts))).toEqual(data);
    // The last piece may be asked for past the end.
    expect((await source.slice(source.size - 10, Infinity)).value.size).toBe(10);
  });

  it('pass other files as they are', async () => {
    const plain = new Blob(['hello']);
    const source = await encryptingReader.openFile(plain);
    expect(source.size).toBe(5);
    const { value, done } = await source.slice(0, 5);
    expect(await value.text()).toBe('hello');
    expect(done).toBe(true);
  });
});
