import { describe, expect, it } from 'vitest';
import checkVectors from '../../contract/crypto/check.json';
import contentVectors from '../../contract/crypto/content.json';
import rfc from '../../contract/crypto/hpke_rfc9180.json';
import lockVectors from '../../contract/crypto/lock.json';
import recoveryVectors from '../../contract/crypto/recovery.json';
import sealVectors from '../../contract/crypto/seal.json';
import { b64u, concat, fromB64u, utf8, type Bytes } from '../src/e2ee/bytes';
import { chunkSize, cipherRange, ContentCipher, decryptChunks, decryptFile, decryptStream, encryptedSize, encryptedSlice, headerSize, newHeader } from '../src/e2ee/content';
import {
  checkCode,
  commitment,
  formatRecoveryCode,
  generateKeyPair,
  importPrivateKey,
  lock,
  newRecoveryCode,
  openKey,
  openThumb,
  parseRecoveryCode,
  passwordLock,
  passwordUnlock,
  purposes,
  recoveryContext,
  sealKey,
  sealThumb,
  secretKey,
  thumbKey,
  unlock,
} from '../src/e2ee/formats';

const hex = (s: string) => new Uint8Array(s.match(/../g)!.map((b) => parseInt(b, 16))) as Bytes;
const toHex = (b: Uint8Array) => [...b].map((x) => x.toString(16).padStart(2, '0')).join('');

/** Bytes i mod 251, as the content vectors' plaintext. */
function plaintext(n: number): Bytes {
  const b = new Uint8Array(n);
  for (let i = 0; i < n; i++) b[i] = i % 251;
  return b;
}

async function sha256(b: Uint8Array): Promise<string> {
  return toHex(new Uint8Array(await crypto.subtle.digest('SHA-256', b as Bytes)));
}

async function encryptAll(fileKey: Bytes, header: Bytes, plain: Bytes): Promise<Bytes> {
  const cipher = await ContentCipher.create(fileKey, header, plain.length);
  return encryptedSlice(cipher, new Blob([plain]), 0, encryptedSize(plain.length));
}

describe('HPKE', () => {
  it("seals and opens RFC 9180's vector", async () => {
    const skE = await importPrivateKey(hex(rfc.skEm), hex(rfc.pkEm));
    const sealed = await sealKey(hex(rfc.pkRm), new TextDecoder().decode(hex(rfc.info)), hex(rfc.aad), hex(rfc.pt), { privateKey: skE, publicKey: hex(rfc.pkEm) });
    expect(toHex(sealed)).toBe(rfc.enc + rfc.ct);
    const skR = await importPrivateKey(hex(rfc.skRm), hex(rfc.pkRm));
    const opened = await openKey({ privateKey: skR, publicKey: hex(rfc.pkRm) }, new TextDecoder().decode(hex(rfc.info)), hex(rfc.aad), sealed);
    expect(toHex(opened)).toBe(rfc.pt);
  });

  it("opens Share's sealed keys, and nothing else", async () => {
    for (const c of sealVectors.open) {
      const pair = { privateKey: await importPrivateKey(fromB64u(c.private_key), fromB64u(c.public_key)), publicKey: fromB64u(c.public_key) };
      expect(b64u(await openKey(pair, c.purpose, utf8(c.aad), fromB64u(c.sealed))), c.name).toBe(c.plaintext);
    }
    for (const c of sealVectors.refuse) {
      const pair = { privateKey: await importPrivateKey(fromB64u(c.private_key), fromB64u(c.public_key)), publicKey: fromB64u(c.public_key) };
      await expect(openKey(pair, c.purpose, utf8(c.aad), fromB64u(c.sealed)), c.name).rejects.toThrow();
    }
  });

  it('seals for fresh keys, which only they open', async () => {
    const folder = await generateKeyPair(true);
    const other = await generateKeyPair(false);
    const aad = utf8('folder:w3dding5x2k7mbqz4bwdbyj6qs:1');
    const fileKey = crypto.getRandomValues(new Uint8Array(32));
    const sealed = await sealKey(folder.publicKey, purposes.file, aad, fileKey);
    expect(sealed.length).toBe(32 + 65 + 16);
    expect(await openKey(folder, purposes.file, aad, sealed)).toEqual(fileKey);
    await expect(openKey(other, purposes.file, aad, sealed)).rejects.toThrow();
    await expect(openKey(folder, purposes.folder, aad, sealed)).rejects.toThrow();
    expect(other.raw).toBeNull();
    await expect(crypto.subtle.exportKey('jwk', other.privateKey)).rejects.toThrow();
  });
});

describe('locks', () => {
  it("open the vectors' secret locks and thumbnail", async () => {
    for (const c of lockVectors.secrets) {
      const key = await secretKey(fromB64u(c.secret), c.purpose);
      expect(b64u(key), c.name).toBe(c.key);
      expect(b64u(await unlock(key, utf8(c.aad), fromB64u(c.locked))), c.name).toBe(c.plaintext);
    }
    const t = lockVectors.thumb;
    expect(b64u(await thumbKey(fromB64u(t.file_key)))).toBe(t.thumb_key);
    expect(b64u(await openThumb(fromB64u(t.file_key), fromB64u(t.sealed)))).toBe(t.plaintext);
    const resealed = await sealThumb(fromB64u(t.file_key), fromB64u(t.plaintext));
    expect(b64u(await openThumb(fromB64u(t.file_key), resealed))).toBe(t.plaintext);
  });

  it("open the vectors' password locks, also of a password with accents and ideographs", async () => {
    for (const c of lockVectors.passwords) {
      expect(b64u(await passwordUnlock(c.password, utf8(c.aad), fromB64u(c.locked))), c.password).toBe(c.plaintext);
      await expect(passwordUnlock(c.password + ' ', utf8(c.aad), fromB64u(c.locked))).rejects.toThrow();
    }
  });

  it('lock and unlock, bound to their aad', async () => {
    const key = await secretKey(utf8('a secret from a link'), purposes.invite);
    const locked = await lock(key, utf8('person:u1'), utf8('folder key'));
    expect(new TextDecoder().decode(await unlock(key, utf8('person:u1'), locked))).toBe('folder key');
    await expect(unlock(key, utf8('person:u2'), locked)).rejects.toThrow();
    const pl = await passwordLock('correct horse', utf8('person:u1'), utf8('person key'), 60_000);
    expect(new TextDecoder().decode(await passwordUnlock('correct horse', utf8('person:u1'), pl))).toBe('person key');
    const weak = await passwordLock('correct horse', utf8('person:u1'), utf8('person key'), 1000);
    await expect(passwordUnlock('correct horse', utf8('person:u1'), weak)).rejects.toThrow();
  });
});

describe('contents', () => {
  const fileKey = fromB64u(contentVectors.file_key);
  const header = fromB64u(contentVectors.header);

  it("encrypt as the vectors' do", async () => {
    for (const c of contentVectors.cases) {
      const enc = await encryptAll(fileKey, header, plaintext(c.size));
      expect(enc.length, `${c.size} bytes`).toBe(c.encrypted_size);
      expect(encryptedSize(c.size)).toBe(c.encrypted_size);
      expect(await sha256(enc), `${c.size} bytes`).toBe(c.sha256);
      if (c.encrypted) expect(b64u(enc)).toBe(c.encrypted);
      const cipher = await ContentCipher.create(fileKey, header, c.size);
      expect(await decryptFile(cipher, enc)).toEqual(plaintext(c.size));
    }
  });

  it('give any range of the encrypted file the same', async () => {
    const plain = plaintext(3 * chunkSize + 77);
    const whole = await encryptAll(fileKey, header, plain);
    const cipher = await ContentCipher.create(fileKey, header, plain.length);
    const blob = new Blob([plain]);
    for (const [start, end] of [
      [0, 5],
      [3, 40],
      [headerSize, headerSize + 1],
      [chunkSize, chunkSize + 100],
      [chunkSize + 30, 2 * chunkSize + 50],
      [100, whole.length],
      [whole.length - 3, whole.length + 10],
    ]) {
      expect(await encryptedSlice(cipher, blob, start, end), `${start}-${end}`).toEqual(whole.subarray(start, end));
    }
  });

  it('decrypt any plain range from the chunks around it', async () => {
    const plain = plaintext(3 * chunkSize + 77);
    const whole = await encryptAll(fileKey, header, plain);
    const cipher = await ContentCipher.create(fileKey, header, plain.length);
    for (const [start, end] of [
      [0, 1],
      [10, chunkSize],
      [chunkSize - 1, chunkSize + 1],
      [2 * chunkSize + 5, plain.length],
    ]) {
      const r = cipherRange(plain.length, start, end);
      const got = await decryptChunks(cipher, r.first, whole.subarray(r.from, r.to));
      expect(got.subarray(r.skip, r.skip + end - start), `${start}-${end}`).toEqual(plain.subarray(start, end));
    }
  });

  it('stream decrypted, and notice tampering', async () => {
    const plain = plaintext(2 * chunkSize + 10);
    const whole = await encryptAll(fileKey, header, plain);
    const cipher = await ContentCipher.create(fileKey, header, plain.length);
    const read = async (bytes: Uint8Array, first = 0) => {
      const parts = [bytes.subarray(0, 1000), bytes.subarray(1000, 70_000), bytes.subarray(70_000)];
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          for (const p of parts) c.enqueue(p);
          c.close();
        },
      }).pipeThrough(decryptStream(cipher, first));
      const out: Uint8Array[] = [];
      const reader = stream.getReader();
      for (let r = await reader.read(); !r.done; r = await reader.read()) out.push(r.value);
      return concat(...out);
    };
    expect(await read(whole.subarray(headerSize))).toEqual(plain);
    expect(await read(whole.subarray(headerSize + chunkSize + 16), 1)).toEqual(plain.subarray(chunkSize));
    const flipped = whole.slice(headerSize);
    flipped[5] ^= 1;
    await expect(read(flipped)).rejects.toThrow();
    await expect(read(whole.subarray(headerSize, headerSize + 2 * (chunkSize + 16)))).rejects.toThrow();
    await expect(ContentCipher.create(fileKey, newHeader().fill(0), 1)).rejects.toThrow();
  });
});

describe('the recovery code', () => {
  it('is written and read as the vectors say', async () => {
    const secret = fromB64u(recoveryVectors.secret);
    expect(formatRecoveryCode(secret)).toBe(recoveryVectors.code);
    for (const s of [recoveryVectors.code, ...recoveryVectors.also_reads]) expect(parseRecoveryCode(s), s).toEqual(secret);
    for (const s of recoveryVectors.refuses) expect(parseRecoveryCode(s), s).toBeNull();
    const key = await secretKey(secret, purposes.recovery);
    expect(b64u(key)).toBe(recoveryVectors.key);
    expect(b64u(await unlock(key, recoveryContext, fromB64u(recoveryVectors.lock.locked)))).toBe(recoveryVectors.lock.private_key);
  });

  it('is new each time, in eight groups of four', () => {
    const { secret, code } = newRecoveryCode();
    expect(code).toMatch(/^([0-9A-HJKMNP-TV-Z]{4}-){7}[0-9A-HJKMNP-TV-Z]{4}$/);
    expect(parseRecoveryCode(code)).toEqual(secret);
  });
});

describe('checks', () => {
  it('commit and make codes as the vectors do', async () => {
    for (const c of checkVectors.cases) {
      expect(b64u(await commitment(fromB64u(c.asker_nonce))), c.name).toBe(c.commitment);
      expect(await checkCode(fromB64u(c.asker_nonce), fromB64u(c.answer_nonce), fromB64u(c.public_key)), c.name).toBe(c.code);
    }
  });
});
