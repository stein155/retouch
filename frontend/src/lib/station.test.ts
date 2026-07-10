import { describe, it, expect } from 'vitest';
import { sameStation, activePresetIndex } from './station';

describe('sameStation', () => {
  it('matches on word boundaries, not substrings', () => {
    expect(sameStation('Radio 1', 'NPO Radio 1')).toBe(true);
    expect(sameStation('Radio 1', 'Radio 10')).toBe(false);
    expect(sameStation('Jazz', 'Jazz')).toBe(true);
    expect(sameStation('', 'x')).toBe(false);
  });
});

describe('activePresetIndex', () => {
  const presets = [
    { name: 'Radio 1', tuneInId: 's1' },
    { name: 'NPO Radio 1', tuneInId: 's2' },
    null,
    { name: 'Jazz FM', tuneInId: 's3' },
  ];

  it('prefers an exact TuneIn-id match', () => {
    expect(activePresetIndex(presets, { name: 'whatever', tuneInId: 's3' })).toBe(3);
  });

  it('prefers an exact name over a looser one (no double highlight)', () => {
    // "NPO Radio 1" is a word-boundary superset of "Radio 1", so a loose match
    // would hit both slots 0 and 1; the exact name must win slot 1 alone.
    expect(activePresetIndex(presets, { name: 'NPO Radio 1', tuneInId: null })).toBe(1);
  });

  it('returns -1 when a loose match is ambiguous', () => {
    // "NPO Radio 1 Extra" word-boundary-contains both "Radio 1" and "NPO Radio 1"
    // and equals neither exactly -> refuse to guess rather than light up two tiles.
    expect(activePresetIndex(presets, { name: 'NPO Radio 1 Extra', tuneInId: null })).toBe(-1);
  });

  it('returns a unique loose match', () => {
    expect(activePresetIndex(presets, { name: 'Jazz FM London', tuneInId: null })).toBe(3);
  });

  it('returns -1 when nothing plays', () => {
    expect(activePresetIndex(presets, null)).toBe(-1);
  });
});
