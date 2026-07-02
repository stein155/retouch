// Live speaker state over Server-Sent Events (STLocal's /api/events).
//
// The server pushes a { now, volume } snapshot the instant playback or volume
// changes, so the UI reacts immediately instead of waiting for the next poll.
// EventSource reconnects on its own (honouring the server's `retry:` hint), so
// onError just marks us offline; useSpeaker then falls back to polling until the
// stream comes back.

import { normalizeNowPlaying } from './api';

// subscribeState opens the stream and returns an unsubscribe function.
// handlers: { onState({ now, volume }), onOpen(), onError() }.
//   now    — normalised now-playing, or undefined when this push didn't include it.
//   volume — a number, or undefined when this push didn't include it.
export function subscribeState({ onState, onOpen, onError } = {}) {
  if (typeof EventSource === 'undefined') {
    return () => {}; // no SSE support: caller keeps polling
  }
  const es = new EventSource('/api/events');
  es.onopen = () => onOpen?.();
  es.onerror = () => onError?.(); // EventSource retries automatically
  es.addEventListener('state', (e) => {
    let data;
    try {
      data = JSON.parse(e.data);
    } catch {
      return;
    }
    onState?.({
      now: 'now' in data ? normalizeNowPlaying(data.now) : undefined,
      volume: typeof data.volume === 'number' ? data.volume : undefined,
    });
  });
  return () => es.close();
}
