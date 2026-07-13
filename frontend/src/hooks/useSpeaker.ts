import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { getNowPlaying, getVolume, getPresets, setVolume } from '../lib/api';
import { subscribeState } from '../lib/events';
import { sameStation } from '../lib/station';
import type { NowPlaying, Preset, Player, PlayerStatus, PlayTarget } from '../lib/types';

type Pending = { name: string; tuneInId: string | null; logo: string; since: number; prevName: string };
type VolumeHold = { value: number; until: number };
type Picked = { name: string; logo: string; tuneInId: string | null };
type Timer = ReturnType<typeof setTimeout>;

// Live state for the single speaker driven through STLocal's /api/* endpoints.
//
// Playback runs through a small state machine so the UI never flickers while a
// station is starting. After a tap the box passes through STANDBY/INVALID then
// BUFFERING_STATE before it reaches PLAY_STATE, and during a switch it may even
// report the *previous* station as PLAY_STATE for a moment. To keep the picked
// station on screen we hold a "pending" target until the box actually plays it
// (or the pending window expires). Components consume a derived { status, station }
// instead of the raw playStatus, so "starten / bufferen / live" render cleanly.

const PENDING_MAX_MS = 15000; // hard cap: give up holding, show reality (likely an error)
const STOP_HOLD_MS = 8000; // after Stop: ignore the box still reporting PLAY_STATE this long
const POLL_ACTIVE_MS = 2500; // poll faster while starting / buffering (SSE down)
const POLL_IDLE_MS = 8000; // settle back to a calm poll when playing / idle (SSE down)
const POLL_SAFETY_MS = 30000; // while SSE is live, only poll occasionally as a backstop
const VOLUME_HOLD_MS = 2500; // ignore polled volume this long after a local change

export function useSpeaker() {
  const [nowPlaying, setNowPlaying] = useState<NowPlaying | null>(null); // raw normalised speaker state
  const [pending, setPending] = useState<Pending | null>(null); // target after a tap
  const [volume, setVolumeState] = useState(20);
  const [presets, setPresets] = useState<(Preset | null)[]>(Array(6).fill(null));
  const [loading, setLoading] = useState(true);

  const pendingRef = useRef<Pending | null>(null);
  pendingRef.current = pending;
  // Volume the user just set locally, held for a moment so a poll that reads the
  // speaker's not-yet-applied volume can't yank the slider back. { value, until }.
  const volumeHoldRef = useRef<VolumeHold | null>(null);
  // What's on screen right now (real or pending), so a tap can remember which
  // station we're switching *away* from.
  const shownNameRef = useRef<string>('');
  // Identity (logo + TuneIn id) of the station we last picked. The speaker's
  // own now-playing reports tuneInId: null and often a blank art right after a
  // switch, so once pending clears we'd otherwise drop back to the initials
  // fallback until TuneIn enrichment warms up. We keep what we already knew and
  // backfill it for the same station so the logo never regresses.
  const lastPickedRef = useRef<Picked | null>(null); // { name, logo, tuneInId }
  // Monotonic id per refresh, so a slow response can't clobber a newer one
  // (refresh is fired concurrently by the poll, nudge timers and action handlers).
  const refreshSeqRef = useRef(0);
  // Set on Stop: while it holds, ignore the box still reporting the old station
  // as playing (its state transitions lag, just like on start).
  const stopHoldRef = useRef(0);
  // Whether the SSE stream is live. While connected, state is pushed and polling
  // drops to a slow safety net; when the stream drops we resume adaptive polling.
  const connectedRef = useRef(false);

  // Fold a fresh reading (from a poll or an SSE push) into local state. np is the
  // normalised now-playing (object / {standby} / null-or-undefined when unknown);
  // vol is a number (or null/undefined when unknown). Both sources funnel through
  // here so the optimistic holds behave identically whichever delivered the update.
  const applyState = useCallback((np: NowPlaying | null | undefined, vol: number | null | undefined) => {
    // Right after Stop the box still reports the old station as PLAY_STATE for a
    // moment; ignore now-playing (but not volume) until it catches up or the hold
    // expires, so the stopped station doesn't pop back on screen.
    const hold = stopHoldRef.current;
    const stillPlaying = np != null && !np.standby && np.playStatus === 'PLAY_STATE';
    const holding = hold && stillPlaying && Date.now() - hold < STOP_HOLD_MS;
    if (hold && !holding) stopHoldRef.current = 0;
    if (np != null && !holding) {
      setNowPlaying(np);
      const p = pendingRef.current;
      if (p) {
        const elapsed = Date.now() - p.since;
        const playing = !np.standby && np.playStatus === 'PLAY_STATE';
        // Clear the hold only once the box actually plays our station, OR has
        // moved off the previous one onto something new — never while it still
        // reports the station we switched away from (that brief PLAY_STATE of the
        // old station during a switch is exactly what used to flicker through).
        const onOurs = sameStation(np.stationName, p.name);
        const stillPrev = sameStation(np.stationName, p.prevName);
        const settled = playing && (onOurs || !stillPrev);
        if (settled || elapsed > PENDING_MAX_MS) setPending(null);
      }
    }
    if (vol != null) {
      // While a local change is held, only accept the speaker's value once it has
      // caught up to what we set (or the hold expires) — otherwise a stale read
      // would make the slider jump back.
      const vhold = volumeHoldRef.current;
      if (vhold && Date.now() < vhold.until && vol !== vhold.value) {
        // keep the optimistic value
      } else {
        volumeHoldRef.current = null;
        setVolumeState(vol);
      }
    }
  }, []);

  const refresh = useCallback(async () => {
    const seq = ++refreshSeqRef.current;
    const [np, vol] = await Promise.all([getNowPlaying(), getVolume()]);
    if (seq !== refreshSeqRef.current) return; // a newer refresh is in flight or landed
    applyState(np, vol);
  }, [applyState]);

  // Dragging the slider fires an onChange per pixel. Sending a POST for each one
  // floods the speaker with out-of-order writes, so the box audibly steps and the
  // slider jumps as stale echoes land. Instead: move the slider instantly (local),
  // and send at most one request at a time — while one is in flight, keep only the
  // latest value and send it next. Serialised (in order) + coalesced (skips the
  // in-between values) + always flushes the final position.
  const volInflightRef = useRef(false);
  const volNextRef = useRef<number | null>(null);
  const changeVolume = useCallback((v: number) => {
    volumeHoldRef.current = { value: v, until: Date.now() + VOLUME_HOLD_MS };
    setVolumeState(v);
    volNextRef.current = v;
    if (volInflightRef.current) return;
    const pump = async () => {
      while (volNextRef.current != null) {
        const val = volNextRef.current;
        volNextRef.current = null;
        volInflightRef.current = true;
        // extend the hold to the value actually being sent, so a poll can't yank
        // the slider back to a not-yet-applied reading mid-drag.
        volumeHoldRef.current = { value: val, until: Date.now() + VOLUME_HOLD_MS };
        try { await setVolume(val); } catch { /* poll / SSE reconciles */ }
      }
      volInflightRef.current = false;
    };
    pump();
  }, []);

  const refreshPresets = useCallback(async () => {
    setPresets(await getPresets());
  }, []);

  // Optimistically show a station as starting the instant the user taps it, so the
  // UI reacts immediately and keeps that station on screen through wake + buffering.
  // station: { name, tuneInId?, logo? }.
  const playOptimistic = useCallback((station: PlayTarget | null) => {
    if (!station) return;
    stopHoldRef.current = 0;
    lastPickedRef.current = {
      name: station.name || '',
      logo: station.logo || '',
      tuneInId: station.tuneInId || null,
    };
    setPending({
      name: station.name || '',
      tuneInId: station.tuneInId || null,
      logo: station.logo || '',
      since: Date.now(),
      prevName: shownNameRef.current, // station we're switching away from
    });
  }, []);

  // Stop: drop any pending target and show idle right away (the box follows shortly).
  const stopOptimistic = useCallback(() => {
    stopHoldRef.current = Date.now();
    setPending(null);
    setNowPlaying({ standby: true });
  }, []);

  // Drop the pending target (e.g. when the play request itself failed), so the
  // UI stops showing "starting" for a station that was never asked to play.
  const cancelPending = useCallback(() => setPending(null), []);

  // Refresh now-playing a few times after an action so the optimistic state
  // converges to the real one (wake + buffering can take a moment). A new nudge
  // supersedes the previous one's remaining timers.
  const nudgeTimersRef = useRef<Timer[]>([]);
  const nudge = useCallback(() => {
    nudgeTimersRef.current.forEach(clearTimeout);
    nudgeTimersRef.current = [500, 1200, 2500, 4500, 7000, 10000, 13000]
      .map((ms) => setTimeout(refresh, ms));
  }, [refresh]);
  // Clear any outstanding nudge timers on unmount so a late refresh can't fetch
  // and setState after the hook is gone.
  useEffect(() => () => nudgeTimersRef.current.forEach(clearTimeout), []);

  // Derived player state the components render. status: idle | starting | buffering | playing.
  const player = useMemo<Player>(() => {
    if (pending) {
      let status: PlayerStatus = 'starting';
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
    let status: PlayerStatus;
    if (ps === 'PLAY_STATE') status = 'playing';
    else if (ps === 'STOP_STATE') return { status: 'idle', station: null };
    else status = 'buffering'; // BUFFERING_STATE or a transient non-standby state
    // The speaker's reported "station name" is often the live TRACK, not the
    // station: ReTouch injects the rolling track onto the box's own display, so the
    // firmware echoes it back in now-playing. Recover the real station name (and
    // logo) from what the app actually picked, or from a preset with the same
    // TuneIn id — matching on the id, which the injection leaves intact, rather
    // than on the name. This keeps the station on line one and the track on line
    // two, instead of the track showing on both. The box's own name is only a last
    // resort (e.g. playback started outside the app, before any track was injected).
    const npId = nowPlaying.tuneInId;
    const picked = lastPickedRef.current;
    const known = picked && (
      (!!npId && picked.tuneInId === npId) || sameStation(nowPlaying.stationName, picked.name)
    ) ? picked : null;
    const presetMatch = npId ? presets.find((p): p is Preset => !!p && p.tuneInId === npId) : undefined;
    // Only trust the box's own name when it isn't just the injected track (that's
    // the duplicate we're avoiding). If it equals the track we have no real station
    // name, so leave it blank and let the player fall back without repeating it.
    const boxName = nowPlaying.stationName && nowPlaying.stationName !== nowPlaying.track
      ? nowPlaying.stationName : '';
    return {
      status,
      station: {
        name: known?.name || presetMatch?.name || boxName || '',
        art: nowPlaying.art || known?.logo || presetMatch?.logo || '',
        tuneInId: npId || known?.tuneInId || null,
        track: nowPlaying.track || '',
        artist: nowPlaying.artist || '',
      },
    };
  }, [pending, nowPlaying, presets]);

  const statusRef = useRef('idle');
  statusRef.current = player.status;
  shownNameRef.current = player.station?.name || '';

  // Initial load.
  useEffect(() => {
    setLoading(true);
    Promise.all([refresh(), refreshPresets()]).finally(() => setLoading(false));
  }, [refresh, refreshPresets]);

  // Live push: the server streams now-playing + volume the instant they change.
  // An SSE push is the freshest truth, so bump the refresh sequence to discard
  // any slower in-flight poll response before folding it in.
  useEffect(() => {
    return subscribeState({
      onState: ({ now, volume }: { now?: NowPlaying; volume?: number }) => {
        refreshSeqRef.current++;
        applyState(now, volume);
      },
      onOpen: () => {
        connectedRef.current = true;
      },
      onError: () => {
        connectedRef.current = false;
      },
    });
  }, [applyState]);

  // Polling: adaptive (quick while starting / buffering) when the SSE stream is
  // down; a slow safety net while it's live, in case a push is ever missed.
  useEffect(() => {
    let timer: Timer;
    let cancelled = false;
    const tick = async () => {
      await refresh();
      if (cancelled) return;
      const active = statusRef.current === 'starting' || statusRef.current === 'buffering';
      const delay = connectedRef.current ? POLL_SAFETY_MS : active ? POLL_ACTIVE_MS : POLL_IDLE_MS;
      timer = setTimeout(tick, delay);
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
    changeVolume,
    playOptimistic,
    stopOptimistic,
    cancelPending,
    nudge,
  };
}
