import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { getNowPlaying, getVolume, getPresets } from '../lib/api';
import { sameStation } from '../lib/station';

// Live state for the single speaker driven through STLocal's /api/* endpoints.
//
// Playback runs through a small state machine so the UI never flickers while a
// station is starting. After a tap the box passes through STANDBY/INVALID then
// BUFFERING_STATE before it reaches PLAY_STATE, and during a switch it may even
// report the *previous* station as PLAY_STATE for a moment. To keep the picked
// station on screen we hold a "pending" target until the box actually plays it
// (or the pending window expires). Components consume a derived { status, station }
// instead of the raw playStatus, so "starten / bufferen / live" render cleanly.

const PENDING_SETTLE_MS = 5000; // after this, any PLAY_STATE is accepted as ours
const PENDING_MAX_MS = 15000; // hard cap: give up holding, show reality (likely an error)
const POLL_ACTIVE_MS = 2500; // poll faster while starting / buffering
const POLL_IDLE_MS = 8000; // settle back to a calm poll when playing / idle

export function useSpeaker() {
  const [nowPlaying, setNowPlaying] = useState(null); // raw normalised speaker state
  const [pending, setPending] = useState(null); // { name, tuneInId, logo, since } target after a tap
  const [volume, setVolumeState] = useState(20);
  const [presets, setPresets] = useState(Array(6).fill(null));
  const [loading, setLoading] = useState(true);

  const pendingRef = useRef(null);
  pendingRef.current = pending;

  const refresh = useCallback(async () => {
    const [np, vol] = await Promise.all([getNowPlaying(), getVolume()]);
    if (np !== null) {
      setNowPlaying(np);
      const p = pendingRef.current;
      if (p) {
        const elapsed = Date.now() - p.since;
        const playing = !np.standby && np.playStatus === 'PLAY_STATE';
        // Clear the hold once the box plays our station (matched by name) or has
        // been playing anything for long enough that the switch must be done.
        const settled = playing && (sameStation(np.stationName, p.name) || elapsed > PENDING_SETTLE_MS);
        if (settled || elapsed > PENDING_MAX_MS) setPending(null);
      }
    }
    if (vol !== null) setVolumeState(vol);
  }, []);

  const refreshPresets = useCallback(async () => {
    setPresets(await getPresets());
  }, []);

  // Optimistically show a station as starting the instant the user taps it, so the
  // UI reacts immediately and keeps that station on screen through wake + buffering.
  // station: { name, tuneInId?, logo? }.
  const playOptimistic = useCallback((station) => {
    if (!station) return;
    setPending({
      name: station.name || '',
      tuneInId: station.tuneInId || null,
      logo: station.logo || '',
      since: Date.now(),
    });
  }, []);

  // Stop: drop any pending target and show idle right away (the box follows shortly).
  const stopOptimistic = useCallback(() => {
    setPending(null);
    setNowPlaying({ standby: true });
  }, []);

  // Refresh now-playing a few times after an action so the optimistic state
  // converges to the real one (wake + buffering can take a moment).
  const nudge = useCallback(() => {
    [500, 1200, 2500, 4500, 7000, 10000, 13000].forEach((ms) => setTimeout(refresh, ms));
  }, [refresh]);

  // Derived player state the components render. status: idle | starting | buffering | playing.
  const player = useMemo(() => {
    if (pending) {
      let status = 'starting';
      if (nowPlaying && !nowPlaying.standby) {
        if (nowPlaying.playStatus === 'BUFFERING_STATE') status = 'buffering';
        else if (nowPlaying.playStatus === 'PLAY_STATE' && sameStation(nowPlaying.stationName, pending.name))
          status = 'playing';
        // else keep 'starting': box is waking or still playing the previous station.
      }
      return {
        status,
        station: { name: pending.name, art: pending.logo, tuneInId: pending.tuneInId, track: '', artist: '' },
      };
    }
    if (!nowPlaying || nowPlaying.standby) return { status: 'idle', station: null };
    const ps = nowPlaying.playStatus;
    let status;
    if (ps === 'PLAY_STATE') status = 'playing';
    else if (ps === 'STOP_STATE') return { status: 'idle', station: null };
    else status = 'buffering'; // BUFFERING_STATE or a transient non-standby state
    return {
      status,
      station: {
        name: nowPlaying.stationName,
        art: nowPlaying.art || '',
        tuneInId: nowPlaying.tuneInId || null,
        track: nowPlaying.track || '',
        artist: nowPlaying.artist || '',
      },
    };
  }, [pending, nowPlaying]);

  const statusRef = useRef('idle');
  statusRef.current = player.status;

  // Initial load.
  useEffect(() => {
    setLoading(true);
    Promise.all([refresh(), refreshPresets()]).finally(() => setLoading(false));
  }, [refresh, refreshPresets]);

  // Adaptive polling: quick while a station is starting / buffering, calm otherwise.
  useEffect(() => {
    let timer;
    let cancelled = false;
    const tick = async () => {
      await refresh();
      if (cancelled) return;
      const active = statusRef.current === 'starting' || statusRef.current === 'buffering';
      timer = setTimeout(tick, active ? POLL_ACTIVE_MS : POLL_IDLE_MS);
    };
    timer = setTimeout(tick, POLL_IDLE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [refresh]);

  return {
    player,
    volume,
    presets,
    loading,
    refreshPresets,
    refreshNowPlaying: refresh,
    setVolumeOptimistic: setVolumeState,
    playOptimistic,
    stopOptimistic,
    nudge,
  };
}
