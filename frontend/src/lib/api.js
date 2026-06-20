// STLocal API client.
//
// Unlike the original multi-speaker app (which talked to each speaker directly
// over http://<ip>:8090 in XML), this build drives a SINGLE speaker through the
// STLocal on-box agent's same-origin JSON API at /api/*. STLocal itself does the
// native TuneIn playback on the speaker.
//
// TuneIn search still goes through the same-origin /api/tunein proxy (STLocal
// adds CORS-free proxying) and the curated catalog is the offline fallback.

async function getJSON(path) {
  const r = await fetch(path, { headers: { Accept: 'application/json' } });
  if (!r.ok) throw new Error(`GET ${path} -> ${r.status}`);
  return r.json();
}

async function send(path, method, body) {
  const r = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!r.ok) {
    let msg = `${method} ${path} -> ${r.status}`;
    try { msg = (await r.json()).error || msg; } catch { /* ignore */ }
    throw new Error(msg);
  }
  return r.status === 204 ? null : r.json().catch(() => null);
}

const clean = (value) => (typeof value === 'string' ? value.trim() : '');

// Now playing — STLocal returns {source,track,artist,station,stationId,art,playStatus}.
// Normalised to the shape the UI expects (standby / stationName / tuneInId).
export async function getNowPlaying() {
  try {
    const np = await getJSON('/api/now');
    const source = clean(np.source);
    if (!source || source === 'STANDBY' || source === 'INVALID_SOURCE') {
      return { standby: true };
    }
    return {
      standby: false,
      source,
      stationName: clean(np.station) || clean(np.track),
      track: clean(np.track),
      artist: clean(np.artist),
      playStatus: clean(np.playStatus),
      art: clean(np.art),
      tuneInId: clean(np.stationId) || null,
    };
  } catch {
    return null;
  }
}

export async function getVolume() {
  try {
    const v = await getJSON('/api/volume');
    return typeof v.volume === 'number' ? v.volume : null;
  } catch {
    return null;
  }
}

// Presets — STLocal returns an array of saved/native presets
// [{slot,name,stationId,location,logo}]. Expanded to a fixed 6-slot array with
// the tuneInId the UI keys off.
export async function getPresets() {
  const slots = Array(6).fill(null);
  try {
    const list = await getJSON('/api/presets');
    for (const p of list || []) {
      const slot = p.slot;
      if (slot >= 1 && slot <= 6) {
        const tuneInId = p.stationId || (p.location || '').match(/station\/(s\d+)/)?.[1] || null;
        slots[slot - 1] = {
          slot,
          name: clean(p.name),
          tuneInId,
          location: p.location || '',
          logo: p.logo || '',
        };
      }
    }
  } catch {
    /* leave slots empty */
  }
  return slots;
}

export async function setVolume(value) {
  return send('/api/volume', 'POST', { volume: value });
}

// Select (play) a TuneIn station by id.
export async function selectStation(tuneInId, name) {
  return send('/api/play', 'POST', { stationId: tuneInId, name: clean(name) });
}

// Play a preset slot. STLocal plays the station saved on that slot server-side.
// Falls back to a direct station select if the slot has a known station id but
// the server-side play fails (e.g. native preset only known to the box).
export async function playPreset(slot, _standby, preset) {
  try {
    return await send(`/api/play/${slot}`, 'POST');
  } catch (e) {
    if (preset?.tuneInId) return selectStation(preset.tuneInId, preset.name);
    throw e;
  }
}

export async function stopPlayback() {
  return send('/api/stop', 'POST');
}

// Save a station to a preset slot (STLocal's local store).
export async function storePreset(slot, tuneInId, name, logo) {
  return send(`/api/presets/${slot}`, 'PUT', { stationId: tuneInId, name: clean(name), logo: logo || '' });
}

// TuneIn OPML search via the same-origin /api/tunein proxy. The original walked
// a nested OPML body; STLocal's tunein proxy mirrors the OPML query so we keep
// the same walk. On failure the UI falls back to the curated catalog.
export async function searchTuneIn(query) {
  try {
    const r = await fetch(
      `/api/tunein/Search.ashx?query=${encodeURIComponent(query)}&types=station&render=json`,
    );
    const data = await r.json();
    const results = [];
    const walk = (items) => {
      if (!Array.isArray(items)) return;
      for (const item of items) {
        const isStation =
          (item.type === 'audio' || item.item === 'station') &&
          (item.guide_id || '').startsWith('s');
        if (isStation) {
          results.push({
            tuneInId: item.guide_id,
            name: item.text || item.name || '',
            tagline: item.subtext || '',
            genre: '',
            country: item.locale?.split('-')?.[1] || '',
            logo: item.image || '',
          });
        }
        if (item.children) walk(item.children);
      }
    };
    walk(data.body);
    return results.slice(0, 30);
  } catch {
    return [];
  }
}

// Settings — speaker name + bass (with range) live on the box; UI language in the
// local store. See STLocal /api/settings.
export async function getSettings() {
  try { return await getJSON('/api/settings'); } catch { return null; }
}

// saveSettings applies any subset of { name, bass, language } (live-apply).
export async function saveSettings(patch) {
  return send('/api/settings', 'PUT', patch);
}

// getVersion returns { version, updatable }. updatable is true only on an installed
// speaker (where ReTouch can replace its own binary). Returns null if unreachable.
export async function getVersion() {
  try { return await getJSON('/api/version'); } catch { return null; }
}

// startUpdate asks the speaker to fetch the latest release and replace itself.
// Returns the server's JSON. On a real update the speaker restarts, so the next
// /api/version may briefly fail until it comes back — the caller polls for that.
export async function startUpdate() {
  const r = await fetch('/api/update', { method: 'POST', headers: { Accept: 'application/json' } });
  let body = null;
  try { body = await r.json(); } catch { /* ignore */ }
  return { ok: r.ok, status: r.status, body: body || {} };
}
