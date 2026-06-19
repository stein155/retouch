// Station name matching. The speaker does not expose the TuneIn id of what's
// live (getNowPlaying returns tuneInId: null), so the UI matches what's playing
// against presets/pending targets by name. Names can differ slightly between
// TuneIn search results and the speaker's stationName (e.g. a "(London)" suffix),
// so the match is loose: equal after normalising, or one clearly contains the other.

export const normName = (s) => (s || '').trim().toLowerCase();

export function sameStation(a, b) {
  const x = normName(a);
  const y = normName(b);
  if (!x || !y) return false;
  if (x === y) return true;
  return x.length > 2 && y.length > 2 && (x.includes(y) || y.includes(x));
}
