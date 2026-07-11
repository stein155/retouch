// STLocal API client.
//
// Unlike the original multi-speaker app (which talked to each speaker directly
// over http://<ip>:8090 in XML), this build drives a SINGLE speaker through the
// STLocal on-box agent's same-origin JSON API at /api/*. STLocal itself does the
// native TuneIn playback on the speaker.
//
// TuneIn search still goes through the same-origin /api/tunein proxy (STLocal
// adds CORS-free proxying) and the curated catalog is the offline fallback.

import type {
  NowPlaying, Preset, Station, Settings, Auth, WifiNetwork, VersionInfo,
  Releases, FoundSpeaker, Multiroom, PluginsResponse, PluginManifest,
  ApiError, UpdateResult, BrowseCategory, BrowseResult,
} from './types';

async function getJSON<T>(path: string): Promise<T> {
  const r = await fetch(path, { headers: { Accept: 'application/json' } });
  if (!r.ok) throw new Error(`GET ${path} -> ${r.status}`);
  return r.json() as Promise<T>;
}

async function send(path: string, method: string, body?: unknown): Promise<unknown> {
  const r = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!r.ok) {
    let msg = `${method} ${path} -> ${r.status}`;
    try { msg = ((await r.json()) as { error?: string }).error || msg; } catch { /* ignore */ }
    const err = new Error(msg) as ApiError;
    err.status = r.status; // 401 = login required; callers show the login view
    throw err;
  }
  return r.status === 204 ? null : r.json().catch(() => null);
}

const clean = (value: unknown): string => (typeof value === 'string' ? value.trim() : '');

// Now playing — STLocal returns {source,track,artist,station,stationId,art,playStatus}.
// Normalised to the shape the UI expects (standby / stationName / tuneInId).
// Shared by both the /api/now fetch and the /api/events push stream (see events.js).
export function normalizeNowPlaying(np: unknown): NowPlaying | null {
  if (!np || typeof np !== 'object') return null;
  const raw = np as Record<string, unknown>;
  const source = clean(raw.source);
  if (!source || source === 'STANDBY' || source === 'INVALID_SOURCE') {
    return { standby: true };
  }
  return {
    standby: false,
    source,
    stationName: clean(raw.station) || clean(raw.track),
    track: clean(raw.track),
    artist: clean(raw.artist),
    playStatus: clean(raw.playStatus),
    art: clean(raw.art),
    tuneInId: clean(raw.stationId) || null,
  };
}

export async function getNowPlaying(): Promise<NowPlaying | null> {
  try {
    return normalizeNowPlaying(await getJSON('/api/now'));
  } catch {
    return null;
  }
}

export async function getVolume(): Promise<number | null> {
  try {
    const v = await getJSON<{ volume?: number }>('/api/volume');
    return typeof v.volume === 'number' ? v.volume : null;
  } catch {
    return null;
  }
}

// Presets — STLocal returns an array of saved/native presets
// [{slot,name,stationId,location,logo}]. Expanded to a fixed 6-slot array with
// the tuneInId the UI keys off.
export async function getPresets(): Promise<(Preset | null)[]> {
  const slots: (Preset | null)[] = Array(6).fill(null);
  try {
    const list = await getJSON<Record<string, unknown>[]>('/api/presets');
    for (const p of list || []) {
      const slot = p.slot as number;
      if (slot >= 1 && slot <= 6) {
        const tuneInId = (p.stationId as string) || (String(p.location || '')).match(/station\/(s\d+)/)?.[1] || null;
        slots[slot - 1] = {
          slot,
          name: clean(p.name),
          tuneInId,
          location: (p.location as string) || '',
          logo: (p.logo as string) || '',
        };
      }
    }
  } catch {
    /* leave slots empty */
  }
  return slots;
}

export async function setVolume(value: number) {
  return send('/api/volume', 'POST', { volume: value });
}

// Select (play) a TuneIn station by id.
export async function selectStation(tuneInId: string, name: string) {
  return send('/api/play', 'POST', { stationId: tuneInId, name: clean(name) });
}

// Play a preset slot. STLocal plays the station saved on that slot server-side.
// Falls back to a direct station select if the slot has a known station id but
// the server-side play fails (e.g. native preset only known to the box).
export async function playPreset(slot: number, _standby: boolean, preset?: Preset | null) {
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
export async function storePreset(slot: number, tuneInId: string, name: string, logo?: string) {
  return send(`/api/presets/${slot}`, 'PUT', { stationId: tuneInId, name: clean(name), logo: logo || '' });
}

// TuneIn OPML search via the same-origin /api/tunein proxy. The original walked
// a nested OPML body; STLocal's tunein proxy mirrors the OPML query so we keep
// the same walk. On failure the UI falls back to the curated catalog.
export async function searchTuneIn(query: string): Promise<Station[]> {
  try {
    const r = await fetch(
      `/api/tunein/Search.ashx?query=${encodeURIComponent(query)}&types=station&render=json`,
    );
    if (!r.ok) throw new Error(`search -> ${r.status}`);
    const data = (await r.json()) as { body?: unknown };
    const results: Station[] = [];
    const walk = (items: unknown) => {
      if (!Array.isArray(items)) return;
      for (const item of items as Record<string, unknown>[]) {
        const station = itemToStation(item);
        if (station) results.push(station);
        if (item.children) walk(item.children);
      }
    };
    walk(data.body);
    return results.slice(0, 30);
  } catch {
    return [];
  }
}

// itemToStation maps a TuneIn OPML "audio" outline to a Station, or null when it
// isn't a real station (no s-prefixed guide id).
function itemToStation(item: Record<string, unknown>): Station | null {
  const isStation = (item.type === 'audio' || item.item === 'station') &&
    String(item.guide_id || '').startsWith('s');
  if (!isStation) return null;
  return {
    tuneInId: item.guide_id as string,
    name: (item.text as string) || (item.name as string) || '',
    tagline: (item.subtext as string) || '',
    genre: '',
    country: String(item.locale || '').split('-')?.[1] || '',
    logo: (item.image as string) || '',
  };
}

// browseTuneIn walks one level of TuneIn's Browse.ashx directory. path is a
// proxy-relative path (defaults to the root browse); it returns the drill-down
// categories and any stations at this level. Empty result on failure so the UI
// falls back to plain search.
export async function browseTuneIn(path = '/Browse.ashx?render=json', locale?: string): Promise<BrowseResult> {
  const categories: BrowseCategory[] = [];
  const stations: Station[] = [];
  try {
    // Force the caller's locale onto every request (not just the root): a child
    // URL that somehow lost it still comes back translated, so labels never
    // revert to English mid-drill.
    let full = path;
    if (locale) {
      const u = new URL(path, 'http://x');
      u.searchParams.set('locale', locale);
      u.searchParams.set('render', 'json');
      full = u.pathname + u.search;
    }
    const r = await fetch(`/api/tunein${full}`);
    if (!r.ok) throw new Error(`browse -> ${r.status}`);
    const data = (await r.json()) as { body?: unknown };
    const walk = (items: unknown) => {
      if (!Array.isArray(items)) return;
      for (const item of items as Record<string, unknown>[]) {
        const station = itemToStation(item);
        if (station) { stations.push(station); continue; }
        // A "link" outline with a URL is a drill-down category (genre/region).
        if (item.type === 'link' && typeof item.URL === 'string' && item.text) {
          const p = browsePath(item.URL);
          if (p) { categories.push({ title: item.text as string, path: p }); continue; }
        }
        // A grouping outline (e.g. "Stations") nests its entries as children.
        if (item.children) walk(item.children);
      }
    };
    walk(data.body);
  } catch { /* leave empty */ }
  return { categories, stations };
}

// browsePath turns an absolute TuneIn Browse URL into a proxy-relative path with
// render=json forced, so it goes back through /api/tunein. Empty on a bad URL.
function browsePath(url: string): string {
  try {
    const u = new URL(url);
    u.searchParams.set('render', 'json');
    // TuneIn's "By Language" node links its Music/Talk/Sports children to dead
    // category ids (id=c424724/5/6) that return "No stations available", while the
    // equivalent c=<category> browse with the same &filter=l<lang> is populated.
    // Rewrite so By Language actually reaches stations.
    // ponytail: hardcoded TuneIn category ids — if TuneIn renumbers them the
    // branch dead-ends again (shows the empty state); revisit if that happens.
    const DEAD_CATEGORY: Record<string, string> = {
      c424724: 'music', c424725: 'talk', c424726: 'sports',
    };
    const id = u.searchParams.get('id');
    if (id && DEAD_CATEGORY[id] && u.searchParams.has('filter')) {
      u.searchParams.delete('id');
      u.searchParams.set('c', DEAD_CATEGORY[id]);
    }
    return u.pathname + u.search;
  } catch {
    return '';
  }
}


// Settings — speaker name + bass (with range) live on the box; UI language in the
// local store. See STLocal /api/settings.
export async function getSettings(): Promise<Settings | null> {
  try { return await getJSON<Settings>('/api/settings'); } catch { return null; }
}

// saveSettings applies any subset of { name, bass, language } (live-apply).
export async function saveSettings(patch: Record<string, unknown>) {
  return send('/api/settings', 'PUT', patch);
}

// Wi-Fi — scan nearby networks and switch the speaker onto one. The scan is a site
// survey on the speaker (slow; show a spinner) and may come back empty on some
// hardware, in which case the UI lets the user type the SSID in. Each network is
// { ssid, signal, secure }.
export async function scanWifi(): Promise<WifiNetwork[]> {
  try { return (await getJSON<{ networks?: WifiNetwork[] }>('/api/wifi/scan')).networks || []; } catch { return []; }
}

// setWifi joins the speaker to a network. security is 'wpa_or_wpa2' | 'wep' | 'none'.
// Changing networks can briefly drop the speaker (and this app) — warn first.
export async function setWifi({ ssid, security, password }: { ssid: string; security: string; password: string }) {
  return send('/api/wifi', 'POST', { ssid, security, password });
}

// Settings login. Settings are open until a password is set; after that the
// settings side of the API answers 401 without a session cookie.

// getAuth returns { hasPassword, authenticated }.
export async function getAuth(): Promise<Auth | null> {
  try { return await getJSON<Auth>('/api/auth'); } catch { return null; }
}

// login exchanges the password for a session cookie. Throws (status 401) on a
// wrong password.
export async function login(password: string) {
  return send('/api/auth/login', 'POST', { password });
}

export async function logout() {
  return send('/api/auth/logout', 'POST');
}

// setPassword sets (no currentPassword needed) or changes the settings password.
export async function setPassword({ currentPassword, newPassword }: { currentPassword: string; newPassword: string }) {
  return send('/api/auth/password', 'POST', { currentPassword, newPassword });
}

// getMqttStatus returns { connected, lastError } for the Home Assistant MQTT link.
export async function getMqttStatus(): Promise<{ connected: boolean; lastError: string } | null> {
  try { return await getJSON('/api/mqtt/status'); } catch { return null; }
}

// Multiroom — native Bose zone grouping. This speaker acts as the zone master;
// other speakers on the network join it and play in sync (Bose's own setZone /
// addZoneSlave / removeZoneSlave under the hood).

// getMultiroom returns { self:{deviceId,name,ip}, isMaster, master, members }.
export async function getMultiroom(): Promise<Multiroom | null> {
  try { return await getJSON<Multiroom>('/api/multiroom'); } catch { return null; }
}

// findSpeakers sweeps the LAN for other SoundTouch speakers. Each row is
// { deviceId, name, model, ip, grouped }. Slow (a network scan), so callers
// should show a spinner. Returns [] on failure.
export async function findSpeakers(): Promise<FoundSpeaker[]> {
  try { return (await getJSON<FoundSpeaker[]>('/api/multiroom/speakers')) || []; } catch { return []; }
}

// groupSpeaker adds the speaker at ip to this speaker's zone (this one master).
export async function groupSpeaker(ip: string) {
  return send('/api/multiroom/group', 'POST', { ip });
}

// ungroupSpeaker removes the speaker at ip from this speaker's zone.
export async function ungroupSpeaker(ip: string) {
  return send('/api/multiroom/ungroup', 'POST', { ip });
}

// getVersion returns { version, updatable }. updatable is true only on an installed
// speaker (where ReTouch can replace its own binary). Returns null if unreachable.
export async function getVersion(): Promise<VersionInfo | null> {
  try { return await getJSON<VersionInfo>('/api/version'); } catch { return null; }
}

// getReleases returns what the speaker can update to:
// { current, updatable, stable: {tag,name}|null, betas: [{tag,pr,name}] }.
// betas are the open-PR builds published by the Beta Build workflow. Returns null
// if unreachable (e.g. offline) so the UI can fall back to the plain Update button.
export async function getReleases(): Promise<Releases | null> {
  try { return await getJSON<Releases>('/api/releases'); } catch { return null; }
}

// --- Plugins --------------------------------------------------------------
// Plugins are separate binaries the speaker downloads, verifies and supervises.
// getPlugins returns { installed: [{name,version,running,lastErr,sideloaded,...}],
// catalog: [{name,title,description,...}] }. Each installed plugin serves its own
// settings UI as a "manifest" that ReTouch proxies under /api/plugins/<name>/.

export async function getPlugins(): Promise<PluginsResponse | null> {
  try { return await getJSON<PluginsResponse>('/api/plugins'); } catch { return null; }
}

// getPluginLatest resolves the newest available release tag for a curated plugin
// ({ tag }), so the UI can offer an over-the-air update. Returns null off-speaker
// or when the release lookup fails (the UI then just hides the update affordance).
export async function getPluginLatest(name: string): Promise<{ tag: string } | null> {
  try { return await getJSON(`/api/plugins/${encodeURIComponent(name)}/latest`); } catch { return null; }
}

export async function installPlugin(name: string, tag?: string) {
  return send(`/api/plugins/${encodeURIComponent(name)}/install`, 'POST', tag ? { tag } : undefined);
}

export async function removePlugin(name: string) {
  return send(`/api/plugins/${encodeURIComponent(name)}`, 'DELETE');
}

// uploadPlugin sideloads a locally-built binary (multipart), for a plugin whose
// release repo is still private.
export async function uploadPlugin(name: string, file: File) {
  const fd = new FormData();
  fd.append('binary', file);
  const r = await fetch(`/api/plugins/${encodeURIComponent(name)}/upload`, { method: 'POST', body: fd });
  if (!r.ok) {
    let msg = `upload -> ${r.status}`;
    try { msg = ((await r.json()) as { error?: string }).error || msg; } catch { /* ignore */ }
    throw new Error(msg);
  }
  return r.json().catch(() => null);
}

// getPluginManifest fetches the plugin's current settings UI (a server-driven
// schema: { title, status, sections:[{fields,rows,actions}] }). Returns null if
// the plugin isn't running yet. The current UI language is passed as ?lang so a
// plugin can localise its own manifest text (status, section copy, action labels).
export async function getPluginManifest(name: string, lang?: string): Promise<PluginManifest | null> {
  const q = lang ? `?lang=${encodeURIComponent(lang)}` : '';
  try { return await getJSON<PluginManifest>(`/api/plugins/${encodeURIComponent(name)}/manifest${q}`); } catch { return null; }
}

// pluginAction performs a manifest action (e.g. log in, submit a 2FA code, save
// devices). The plugin replies with the NEW manifest, which the UI re-renders —
// that's how multi-step flows like 2FA fall out without any plugin-specific code.
// lang is forwarded as ?lang so the returned manifest stays in the UI language.
export async function pluginAction(name: string, id: string, body?: unknown, lang?: string) {
  const q = lang ? `?lang=${encodeURIComponent(lang)}` : '';
  return send(`/api/plugins/${encodeURIComponent(name)}/action/${encodeURIComponent(id)}${q}`, 'POST', body || {});
}

// startUpdate asks the speaker to fetch a release and replace itself. With no tag
// it installs the latest stable; pass a beta tag (e.g. "beta-pr-12") to install
// that one instead. On a real update the speaker restarts, so the next
// /api/version may briefly fail until it comes back — the caller polls for that.
export async function startUpdate(tag?: string): Promise<UpdateResult> {
  const r = await fetch('/api/update', {
    method: 'POST',
    headers: { Accept: 'application/json', ...(tag ? { 'Content-Type': 'application/json' } : {}) },
    body: tag ? JSON.stringify({ tag }) : undefined,
  });
  let body: UpdateResult['body'] | null = null;
  try { body = await r.json(); } catch { /* ignore */ }
  return { ok: r.ok, status: r.status, body: body || {} };
}
