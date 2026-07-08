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
  normalizeNowPlaying: (x) => x,
}));
vi.mock('../lib/events', () => ({ subscribeState: () => () => {} }));

describe('useSpeaker changeVolume', () => {
  beforeEach(() => vi.clearAllMocks());

  it('serialises writes and skips intermediate values while one is in flight', async () => {
    // First write hangs so 20 and 30 pile up behind it; only the latest survives.
    let release;
    api.setVolume
      .mockImplementationOnce(() => new Promise((r) => { release = r; }))
      .mockResolvedValue();

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
