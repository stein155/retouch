import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { useSpeaker } from './useSpeaker';
import * as api from '../lib/api';

// Only the volume-coalescing path is exercised here; the rest of the hook is
// covered end-to-end by the app integration test.
vi.mock('../lib/api', () => ({
  getNowPlaying: vi.fn().mockResolvedValue({ standby: true }),
  getVolume: vi.fn().mockResolvedValue(20),
  getPresets: vi.fn().mockResolvedValue(Array(6).fill(null)),
  setVolume: vi.fn(),
  normalizeNowPlaying: (x: unknown) => x,
}));
vi.mock('../lib/events', () => ({ subscribeState: () => () => {} }));

describe('useSpeaker changeVolume', () => {
  beforeEach(() => vi.clearAllMocks());

  it('serialises writes and skips intermediate values while one is in flight', async () => {
    // First write hangs so 20 and 30 pile up behind it; only the latest survives.
    let release: () => void = () => {};
    vi.mocked(api.setVolume)
      .mockImplementationOnce(() => new Promise<void>((r) => { release = r; }))
      .mockResolvedValue(undefined);

    const { result } = renderHook(() => useSpeaker());

    act(() => { result.current.changeVolume(10); }); // sent immediately
    act(() => { result.current.changeVolume(20); }); // coalesced away
    act(() => { result.current.changeVolume(30); }); // latest — sent next

    expect(api.setVolume).toHaveBeenCalledTimes(1);
    expect(api.setVolume).toHaveBeenNthCalledWith(1, 10);

    await act(async () => { release(); });

    await waitFor(() => expect(api.setVolume).toHaveBeenCalledTimes(2));
    expect(api.setVolume).toHaveBeenNthCalledWith(2, 30); // 20 was dropped
  });
});

describe('useSpeaker optimistic playback', () => {
  beforeEach(() => vi.clearAllMocks());

  it('shows a picked station as "starting" the instant it is tapped', async () => {
    const { result } = renderHook(() => useSpeaker());
    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() => { result.current.playOptimistic({ name: 'Jazz FM', tuneInId: 's1', logo: 'l' }); });

    expect(result.current.player.status).toBe('starting');
    expect(result.current.player.station?.name).toBe('Jazz FM');
    expect(result.current.player.station?.tuneInId).toBe('s1');
  });

  it('collapses to idle immediately on stop', async () => {
    const { result } = renderHook(() => useSpeaker());
    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() => { result.current.playOptimistic({ name: 'Jazz FM', tuneInId: 's1' }); });
    expect(result.current.player.status).toBe('starting');

    act(() => { result.current.stopOptimistic(); });
    expect(result.current.player.status).toBe('idle');
    expect(result.current.player.station).toBeNull();
  });

  it('drops the pending target when cancelled (failed play)', async () => {
    const { result } = renderHook(() => useSpeaker());
    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() => { result.current.playOptimistic({ name: 'Jazz FM', tuneInId: 's1' }); });
    act(() => { result.current.cancelPending(); });
    expect(result.current.player.status).toBe('idle');
  });
});
