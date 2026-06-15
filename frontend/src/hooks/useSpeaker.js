import { useState, useEffect, useCallback, useRef } from 'react';
import { getNowPlaying, getVolume, getPresets } from '../lib/api';

// Live state for the single speaker driven through STLocal's /api/* endpoints.
export function useSpeaker() {
  const [nowPlaying, setNowPlaying] = useState(null);
  const [volume, setVolumeState] = useState(20);
  const [presets, setPresets] = useState(Array(6).fill(null));
  const [loading, setLoading] = useState(true);
  // While a tapped station is starting, the box briefly reports STANDBY/buffering
  // before PLAY_STATE. Hold the optimistic "playing" state until then so the UI
  // doesn't flicker back to "choose a station".
  const holdUntil = useRef(0);

  const refresh = useCallback(async () => {
    const [np, vol] = await Promise.all([getNowPlaying(), getVolume()]);
    if (np !== null) {
      // After a tap the box passes through STANDBY then BUFFERING_STATE before it
      // reaches PLAY_STATE. Until it actually reports PLAY_STATE (or the hold
      // window expires), keep the optimistic "playing" state so the UI doesn't
      // flicker back to "choose a station" while the stream is starting.
      const notPlayingYet = np.standby || np.playStatus !== 'PLAY_STATE';
      const holding = notPlayingYet && Date.now() < holdUntil.current;
      if (!holding) setNowPlaying(np); // else keep the optimistic state
    }
    if (vol !== null) setVolumeState(vol);
  }, []);

  const refreshPresets = useCallback(async () => {
    setPresets(await getPresets());
  }, []);

  // Optimistically show a station as playing the instant the user taps it, so the
  // UI reacts immediately instead of waiting for the next poll. station: {name,
  // tuneInId?, logo?}.
  const playOptimistic = useCallback((station) => {
    if (!station) return;
    holdUntil.current = Date.now() + 10000; // hold through wake + buffering (~10s)
    setNowPlaying({
      standby: false,
      playStatus: 'PLAY_STATE',
      stationName: station.name || '',
      tuneInId: station.tuneInId || null,
      art: station.logo || '',
    });
  }, []);

  // Refresh now-playing a few times quickly after an action so the optimistic
  // state converges to the real one (wake + buffering can take a moment).
  const nudge = useCallback(() => {
    [400, 1200, 2800, 5000, 7500, 9500].forEach((ms) => setTimeout(refresh, ms));
  }, [refresh]);

  // Initial load.
  useEffect(() => {
    setLoading(true);
    Promise.all([refresh(), refreshPresets()]).finally(() => setLoading(false));
  }, [refresh, refreshPresets]);

  // Poll now_playing + volume every 8s.
  useEffect(() => {
    const t = setInterval(refresh, 8000);
    return () => clearInterval(t);
  }, [refresh]);

  return {
    nowPlaying,
    volume,
    presets,
    loading,
    refreshPresets,
    refreshNowPlaying: refresh,
    setVolumeOptimistic: setVolumeState,
    playOptimistic,
    nudge,
  };
}
