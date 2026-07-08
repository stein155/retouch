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
  if (x.length <= 2 || y.length <= 2) return false;
  return containsWord(x, y) || containsWord(y, x);
}

// Containment counts only on word boundaries, so "radio 1" matches
// "npo radio 1" but never "radio 10".
const alnum = /[a-z0-9]/;
function containsWord(long, short) {
  for (let i = long.indexOf(short); i !== -1; i = long.indexOf(short, i + 1)) {
    const okBefore = i === 0 || !alnum.test(long[i - 1]);
    const okAfter = i + short.length === long.length || !alnum.test(long[i + short.length]);
    if (okBefore && okAfter) return true;
  }
  return false;
}
