import { describe, it, expect, vi, afterEach } from 'vitest';
import {
  normalizeNowPlaying, getVolume, getPresets, searchTuneIn,
} from './api';

// Stub global.fetch with a JSON response for the fetch-backed helpers.
function mockFetch(body: unknown, { ok = true, status = 200 }: { ok?: boolean; status?: number } = {}) {
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok, status,
    json: async () => body,
  })) as unknown as typeof fetch);
}

afterEach(() => vi.unstubAllGlobals());

describe('normalizeNowPlaying', () => {
  it('returns null for empty input', () => {
    expect(normalizeNowPlaying(null)).toBeNull();
    expect(normalizeNowPlaying(undefined)).toBeNull();
  });

  it('maps standby / invalid sources to { standby: true }', () => {
    expect(normalizeNowPlaying({ source: 'STANDBY' })).toEqual({ standby: true });
    expect(normalizeNowPlaying({ source: 'INVALID_SOURCE' })).toEqual({ standby: true });
    expect(normalizeNowPlaying({ source: '' })).toEqual({ standby: true });
  });

  it('normalises a playing station (trims, maps stationId -> tuneInId)', () => {
    const np = normalizeNowPlaying({
      source: 'TUNEIN', station: '  Jazz FM ', track: ' Song ', artist: ' Artist ',
      playStatus: 'PLAY_STATE', art: 'http://a', stationId: 's42',
    });
    expect(np).toEqual({
      standby: false, source: 'TUNEIN', stationName: 'Jazz FM', track: 'Song',
      artist: 'Artist', playStatus: 'PLAY_STATE', art: 'http://a', tuneInId: 's42',
    });
  });

  it('falls back to the track for the station name and null tuneInId', () => {
    const np = normalizeNowPlaying({ source: 'TUNEIN', track: 'Only Track' });
    expect(np).toMatchObject({ stationName: 'Only Track', tuneInId: null });
  });
});

describe('getVolume', () => {
  it('returns the numeric volume', async () => {
    mockFetch({ volume: 42 });
    expect(await getVolume()).toBe(42);
  });
  it('returns null on a non-numeric / failed response', async () => {
    mockFetch({});
    expect(await getVolume()).toBeNull();
  });
});

describe('getPresets', () => {
  it('expands to a fixed 6-slot array keyed by slot', async () => {
    mockFetch([
      { slot: 1, name: 'A', stationId: 's1', location: '/v1/playback/station/s1', logo: 'l1' },
      { slot: 3, name: 'C', location: '/v1/playback/station/s9' }, // tuneInId from location
      { slot: 7, name: 'X' }, // out of range -> ignored
    ]);
    const slots = await getPresets();
    expect(slots).toHaveLength(6);
    expect(slots[0]).toMatchObject({ slot: 1, name: 'A', tuneInId: 's1', logo: 'l1' });
    expect(slots[2]).toMatchObject({ slot: 3, name: 'C', tuneInId: 's9' }); // parsed from location
    expect(slots[1]).toBeNull();
    expect(slots[5]).toBeNull(); // slot 7 dropped
  });

  it('returns six empty slots when the request fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) })) as unknown as typeof fetch);
    expect(await getPresets()).toEqual(Array(6).fill(null));
  });
});

describe('searchTuneIn', () => {
  it('flattens nested OPML, keeps only s-prefixed stations, caps at 30', async () => {
    const items = Array.from({ length: 40 }, (_, i) => ({
      type: 'audio', guide_id: `s${i}`, text: `Station ${i}`, image: `img${i}`,
    }));
    // A non-station and a nested child to exercise the walker.
    mockFetch({ body: [
      { type: 'link', guide_id: 'x1', text: 'not a station' },
      { children: items },
    ] });
    const res = await searchTuneIn('jazz');
    expect(res).toHaveLength(30); // capped
    expect(res[0]).toMatchObject({ tuneInId: 's0', name: 'Station 0', logo: 'img0' });
    expect(res.some((s) => s.tuneInId === 'x1')).toBe(false);
  });

  it('returns [] on a failed search', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 502, json: async () => ({}) })) as unknown as typeof fetch);
    expect(await searchTuneIn('x')).toEqual([]);
  });
});
