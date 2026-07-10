// Shared domain types for the ReTouch UI. The API client (api.ts) produces these;
// hooks and components consume them. Kept in one place so the shapes stay in sync.

// Normalised now-playing. `standby` discriminates: when true nothing else is set.
export type NowPlaying =
  | { standby: true }
  | {
      standby: false;
      source: string;
      stationName: string;
      track: string;
      artist: string;
      playStatus: string;
      art: string;
      tuneInId: string | null;
    };

// One preset slot (1..6), or null for an empty slot.
export interface Preset {
  slot: number;
  name: string;
  tuneInId: string | null;
  location: string;
  logo: string;
}

// A TuneIn search result / catalog station.
export interface Station {
  tuneInId: string;
  name: string;
  tagline: string;
  genre: string;
  country: string;
  logo: string;
}

// Derived playback state the UI renders (see useSpeaker).
export type PlayerStatus = 'idle' | 'starting' | 'buffering' | 'playing';

export interface PlayerStation {
  name: string;
  art: string;
  tuneInId: string | null;
  track: string;
  artist: string;
}

export interface Player {
  status: PlayerStatus;
  station: PlayerStation | null;
}

// A tap target passed to playOptimistic: a preset or a search result.
export interface PlayTarget {
  name: string;
  tuneInId?: string | null;
  logo?: string;
}

export interface BassCaps {
  actual: number;
  min: number;
  max: number;
  default: number;
}

export interface ToneCaps {
  value: number;
  min: number;
  max: number;
  step: number;
}

export interface NetworkInfo {
  type: string;
  ssid: string;
  signal: string;
  ip: string;
}

export interface MqttConfig {
  enabled: boolean;
  host: string;
  port: number;
  username: string;
  hasPassword: boolean;
  baseTopic: string;
  discoveryPrefix: string;
  tls: boolean;
  connected?: boolean;
  lastError?: string;
}

// GET /api/settings — every field beyond language/name is device- or
// auth-gated, so most are optional.
export interface Settings {
  language?: string;
  name?: string;
  model?: string;
  hasPassword: boolean;
  authenticated: boolean;
  bass?: BassCaps;
  treble?: ToneCaps;
  wifiOptimization?: boolean;
  closeTelnet?: boolean;
  network?: NetworkInfo;
  host?: string;
  mqtt?: MqttConfig;
}

export interface Auth {
  hasPassword: boolean;
  authenticated: boolean;
}

export interface WifiNetwork {
  ssid: string;
  signal: string;
  secure: boolean;
}

export interface VersionInfo {
  version: string;
  updatable: boolean;
}

export interface ReleaseRef {
  tag: string;
  name: string;
  pr?: number;
}

export interface Releases {
  current: string;
  updatable: boolean;
  stable: ReleaseRef | null;
  betas: ReleaseRef[];
}

export interface FoundSpeaker {
  deviceId: string;
  name: string;
  model: string;
  ip: string;
  grouped: boolean;
}

export interface Multiroom {
  self: { deviceId: string; name: string; ip: string };
  isMaster: boolean;
  master?: string;
  members: { deviceId: string; ip: string }[];
}

// --- Plugins --------------------------------------------------------------

export interface InstalledPluginInfo {
  name: string;
  version: string;
  running: boolean;
  lastErr?: string;
  sideloaded?: boolean;
  [k: string]: unknown;
}

export interface CatalogPluginInfo {
  name: string;
  title: string;
  description: string;
  [k: string]: unknown;
}

export interface PluginsResponse {
  installed: InstalledPluginInfo[];
  catalog: CatalogPluginInfo[];
}

// A plugin's settings UI is a server-driven schema; fields/actions are dynamic,
// so values stay loosely typed on purpose.
export interface ManifestField {
  id: string;
  label?: string;
  type?: string;
  value?: unknown;
  [k: string]: unknown;
}

export interface ManifestAction {
  id: string;
  label?: string;
  [k: string]: unknown;
}

export interface ManifestSection {
  title?: string;
  fields?: ManifestField[];
  rows?: unknown[];
  actions?: ManifestAction[];
  [k: string]: unknown;
}

export interface PluginManifest {
  title?: string;
  status?: string;
  sections?: ManifestSection[];
  [k: string]: unknown;
}

// send()/getJSON() reject with an Error carrying the HTTP status (401 = login).
export interface ApiError extends Error {
  status?: number;
}

// startUpdate resolves to the raw response shape (not thrown on non-2xx).
export interface UpdateResult {
  ok: boolean;
  status: number;
  body: { status?: string; to?: string; error?: string; [k: string]: unknown };
}
