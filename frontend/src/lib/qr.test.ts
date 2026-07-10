import { describe, it, expect } from 'vitest';
import { encode } from './qr';

// Golden matrix for a representative HomeKit setup URI (version 2, level M). It was
// captured from this encoder and independently confirmed to decode with a real QR
// reader (jsQR). Any change to the encoding pipeline — Reed–Solomon, interleaving,
// masking, format bits, module placement — that alters the output flips this test,
// which is the point: a silently-unscannable regression would otherwise slip by.
const GOLDEN = [
  '1111111010000110101111111', '1000001000011110001000001', '1011101001011011101011101',
  '1011101011001010001011101', '1011101011100111001011101', '1000001010111010101000001',
  '1111111010101010101111111', '0000000011001001100000000', '1000101111111101001111001',
  '0011100100000110110110101', '0101001100100101010001100', '1001110011011011111000100',
  '1011001000011100101000010', '1111010111100111010111100', '0011011000100011111000110',
  '0010100010001000110001110', '1110111000010110111111100', '0000000011101011100010111',
  '1111111011011111101011000', '1000001001110100100011100', '1011101010101100111110111',
  '1011101000000110101001111', '1011101000100000001000010', '1000001001101011100111110',
  '1111111011010111001000111',
];

describe('qr.encode', () => {
  it('matches the golden matrix for a HomeKit setup URI', () => {
    const { size, modules } = encode('X-HM://00284NR1I8ABCD');
    expect(size).toBe(25); // version 2
    const rows = modules.map((r) => r.map((b) => (b ? 1 : 0)).join(''));
    expect(rows).toEqual(GOLDEN);
  });

  it('picks the smallest version that fits', () => {
    expect(encode('hi').size).toBe(21); // version 1
    expect(encode('X'.repeat(20)).size).toBe(25); // version 2
  });

  it('places the three finder patterns', () => {
    const { size, modules } = encode('hello');
    for (const [r0, c0] of [[0, 0], [0, size - 7], [size - 7, 0]]) {
      expect(modules[r0][c0]).toBe(true);
      expect(modules[r0 + 1][c0 + 1]).toBe(false); // finder inner ring
      expect(modules[r0 + 3][c0 + 3]).toBe(true); // finder centre
    }
  });

  it('throws rather than emit an unscannable code past its version range', () => {
    expect(() => encode('x'.repeat(200))).toThrow(/too long/);
  });
});
