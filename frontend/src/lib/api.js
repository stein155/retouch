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
    const err = new Error(msg);
    err.status = r.status; // 401 = login required; callers show the login view
    throw err;
  }
  return r.status === 204 ? null : r.json().catch(() => null);
}

const clean = (value) => (typeof value === 'string' ? value.trim() : '');

// Now playing — STLocal returns {source,track,artist,station,stationId,art,playStatus}.
// Normalised to the shape the UI expects (standby / stationName / tuneInId).
// Shared by both the /api/now fetch and the /api/events push stream (see events.js).
export function normalizeNowPlaying(np) {
  if (!np) return null;
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
}

export async function getNowPlaying() {
  try {
    return normalizeNowPlaying(await getJSON('/api/now'));
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

// Wi-Fi — scan nearby networks and switch the speaker onto one. The scan is a site
// survey on the speaker (slow; show a spinner) and may come back empty on some
// hardware, in which case the UI lets the user type the SSID in. Each network is
// { ssid, signal, secure }.
export async function scanWifi() {
  try { return (await getJSON('/api/wifi/scan'))?.networks || []; } catch { return []; }
}

// setWifi joins the speaker to a network. security is 'wpa_or_wpa2' | 'wep' | 'none'.
// Changing networks can briefly drop the speaker (and this app) — warn first.
export async function setWifi({ ssid, security, password }) {
  return send('/api/wifi', 'POST', { ssid, security, password });
}

// Settings login. Settings are open until a password is set; after that the
// settings side of the API answers 401 without a session cookie.

// getAuth returns { hasPassword, authenticated }.
export async function getAuth() {
  try { return await getJSON('/api/auth'); } catch { return null; }
}

// login exchanges the password for a session cookie. Throws (status 401) on a
// wrong password.
export async function login(password) {
  return send('/api/auth/login', 'POST', { password });
}

export async function logout() {
  return send('/api/auth/logout', 'POST');
}

// setPassword sets (no currentPassword needed) or changes the settings password.
export async function setPassword({ currentPassword, newPassword }) {
  return send('/api/auth/password', 'POST', { currentPassword, newPassword });
}

// getMqttStatus returns { connected, lastError } for the Home Assistant MQTT link.
export async function getMqttStatus() {
  try { return await getJSON('/api/mqtt/status'); } catch { return null; }
}

// Multiroom — native Bose zone grouping. This speaker acts as the zone master;
// other speakers on the network join it and play in sync (Bose's own setZone /
// addZoneSlave / removeZoneSlave under the hood).

// getMultiroom returns { self:{deviceId,name,ip}, isMaster, master, members }.
export async function getMultiroom() {
  try { return await getJSON('/api/multiroom'); } catch { return null; }
}

// findSpeakers sweeps the LAN for other SoundTouch speakers. Each row is
// { deviceId, name, model, ip, grouped }. Slow (a network scan), so callers
// should show a spinner. Returns [] on failure.
export async function findSpeakers() {
  try { return (await getJSON('/api/multiroom/speakers')) || []; } catch { return []; }
}

// groupSpeaker adds the speaker at ip to this speaker's zone (this one master).
export async function groupSpeaker(ip) {
  return send('/api/multiroom/group', 'POST', { ip });
}

// ungroupSpeaker removes the speaker at ip from this speaker's zone.
export async function ungroupSpeaker(ip) {
  return send('/api/multiroom/ungroup', 'POST', { ip });
}

// getVersion returns { version, updatable }. updatable is true only on an installed
// speaker (where ReTouch can replace its own binary). Returns null if unreachable.
export async function getVersion() {
  try { return await getJSON('/api/version'); } catch { return null; }
}

// getReleases returns what the speaker can update to:
// { current, updatable, stable: {tag,name}|null, betas: [{tag,pr,name}] }.
// betas are the open-PR builds published by the Beta Build workflow. Returns null
// if unreachable (e.g. offline) so the UI can fall back to the plain Update button.
export async function getReleases() {
  try { return await getJSON('/api/releases'); } catch { return null; }
}

// --- Plugins --------------------------------------------------------------
// Plugins are separate binaries the speaker downloads, verifies and supervises.
// getPlugins returns { installed: [{name,version,running,lastErr,sideloaded,...}],
// catalog: [{name,title,description,...}] }. Each installed plugin serves its own
// settings UI as a "manifest" that ReTouch proxies under /api/plugins/<name>/.

export async function getPlugins() {
  try { return await getJSON('/api/plugins'); } catch { return null; }
}

export async function installPlugin(name, tag) {
  return send(`/api/plugins/${encodeURIComponent(name)}/install`, 'POST', tag ? { tag } : undefined);
}

export async function removePlugin(name) {
  return send(`/api/plugins/${encodeURIComponent(name)}`, 'DELETE');
}

// uploadPlugin sideloads a locally-built binary (multipart), for a plugin whose
// release repo is still private.
export async function uploadPlugin(name, file) {
  const fd = new FormData();
  fd.append('binary', file);
  const r = await fetch(`/api/plugins/${encodeURIComponent(name)}/upload`, { method: 'POST', body: fd });
  if (!r.ok) {
    let msg = `upload -> ${r.status}`;
    try { msg = (await r.json()).error || msg; } catch { /* ignore */ }
    throw new Error(msg);
  }
  return r.json().catch(() => null);
}

// getPluginManifest fetches the plugin's current settings UI (a server-driven
// schema: { title, status, sections:[{fields,rows,actions}] }). Returns null if
// the plugin isn't running yet.
export async function getPluginManifest(name) {
  try { return await getJSON(`/api/plugins/${encodeURIComponent(name)}/manifest`); } catch { return null; }
}

// pluginAction performs a manifest action (e.g. log in, submit a 2FA code, save
// devices). The plugin replies with the NEW manifest, which the UI re-renders —
// that's how multi-step flows like 2FA fall out without any plugin-specific code.
export async function pluginAction(name, id, body) {
  return send(`/api/plugins/${encodeURIComponent(name)}/action/${encodeURIComponent(id)}`, 'POST', body || {});
}

// startUpdate asks the speaker to fetch a release and replace itself. With no tag
// it installs the latest stable; pass a beta tag (e.g. "beta-pr-12") to install
// that one instead. On a real update the speaker restarts, so the next
// /api/version may briefly fail until it comes back — the caller polls for that.
export async function startUpdate(tag) {
  const r = await fetch('/api/update', {
    method: 'POST',
    headers: { Accept: 'application/json', ...(tag ? { 'Content-Type': 'application/json' } : {}) },
    body: tag ? JSON.stringify({ tag }) : undefined,
  });
  let body = null;
  try { body = await r.json(); } catch { /* ignore */ }
  return { ok: r.ok, status: r.status, body: body || {} };
}
