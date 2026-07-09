import { useState, useCallback, useMemo, useEffect, useRef } from 'react';
import { useSpeaker } from '../hooks/useSpeaker';
import { Header } from '../components/organisms/Header';
import { PresetGrid } from '../components/organisms/PresetGrid';
import { BrowseRow } from '../components/organisms/BrowseRow';
import { MiniPlayer } from '../components/organisms/MiniPlayer';
import { SearchSheet } from '../components/organisms/SearchSheet';
import { SettingsSheet } from '../components/organisms/SettingsSheet';
import { Shell, ShellScroll, Footer, FooterBose } from '../components/templates/AppShell';
import {
  playPreset, stopPlayback, storePreset, selectStation, getSettings, saveSettings,
} from '../lib/api';
import { makeT, I18nContext, useI18n, detectInitialLang, LANGS } from '../lib/i18n';
import { useThemeMode } from '../theme/themeMode';

const clean = (value) => (typeof value === 'string' ? value.trim() : '');

// The page body. Reads translations from context; everything else comes in as
// props from HomePage. This is App.jsx's old body, unchanged in behaviour.
function HomeBody({
  lang, onSetLang, themeMode, onSetTheme, speakerName, setSpeakerName, speakerModel,
  search, setSearch, settingsOpen, setSettingsOpen, data, settingsLoaded,
}) {
  const { t } = useI18n();

  // Header turns into a translucent fade-out scrim only once the body is
  // scrolled; at the top it stays a plain solid bar.
  const [scrolled, setScrolled] = useState(false);
  const handleScroll = useCallback((e) => {
    const s = e.currentTarget.scrollTop > 4;
    setScrolled((prev) => (prev !== s ? s : prev));
  }, []);

  // A tap can register twice (touch then click, or an impatient double-tap),
  // firing two play/select requests that fight each other and make the tile flicker
  // between "starting" states. Ignore a repeat tap on the station already being
  // switched to while its request is still settling.
  const switchingRef = useRef(null); // name currently being switched to, or null
  const isDupTap = useCallback((name) => {
    const cur = clean(name);
    if (switchingRef.current === cur) return true;
    switchingRef.current = cur;
    return false;
  }, []);

  const handlePlay = useCallback(async (preset, slot) => {
    if (isDupTap(preset?.name)) return;
    const standby = data.player.status !== 'playing';
    data.playOptimistic(preset); // show it instantly
    data.nudge();
    try {
      await playPreset(slot, standby, preset);
    } catch {
      data.cancelPending(); // request never reached the box: stop showing "starting"
    }
    switchingRef.current = null;
    data.refreshNowPlaying();
  }, [data, isDupTap]);

  const handleStop = useCallback(async () => {
    data.stopOptimistic(); // collapse to idle right away
    try {
      await stopPlayback();
    } catch { /* the poll reconciles with the box's real state */ }
    setTimeout(data.refreshNowPlaying, 1000);
  }, [data]);

  const handleVolume = useCallback((v) => data.changeVolume(v), [data]);

  const handleAssign = useCallback((slot) => setSearch({ mode: 'assign', slot }), [setSearch]);

  const handlePick = useCallback(async (station) => {
    // Stations come straight from live TuneIn search, so the id is always
    // current (no hardcoded catalog ids that could go stale).
    if (!station.tuneInId) { setSearch(null); return; }
    if (isDupTap(station.name)) { setSearch(null); return; }
    const assignSlot = search?.mode === 'assign' ? search.slot : null;
    data.playOptimistic(station); // show it instantly
    data.nudge();
    setSearch(null);
    try {
      if (assignSlot != null) {
        await storePreset(assignSlot, station.tuneInId, station.name, station.logo);
        await data.refreshPresets();
      }
      await selectStation(station.tuneInId, station.name);
    } catch {
      data.cancelPending(); // request failed: stop showing "starting"
    }
    switchingRef.current = null;
    data.refreshNowPlaying();
  }, [search, data, setSearch, isDupTap]);

  return (
    <Shell>
      <Header onSettings={() => setSettingsOpen(true)} scrolled={scrolled} />

      <ShellScroll onScroll={handleScroll}>
        <PresetGrid
          presets={data.presets}
          player={data.player}
          loading={data.loading}
          onPlay={handlePlay}
          onAssign={handleAssign}
        />

        <BrowseRow onClick={() => setSearch({ mode: 'browse' })} />

        <Footer>
          <span>{t('connectedWith')}</span>
          <FooterBose>{speakerModel ? `Bose ${speakerModel}` : 'Bose'}</FooterBose>
        </Footer>
      </ShellScroll>

      <MiniPlayer
        player={data.player}
        volume={data.volume}
        speakerName={speakerName}
        loading={data.loading || !settingsLoaded}
        onStop={handleStop}
        onVolume={handleVolume}
      />

      <SearchSheet
        open={!!search}
        mode={search}
        speakerName={speakerName}
        onClose={() => setSearch(null)}
        onPick={handlePick}
      />

      <SettingsSheet
        open={settingsOpen}
        lang={lang}
        onSetLang={onSetLang}
        themeMode={themeMode}
        onSetTheme={onSetTheme}
        onNameChange={setSpeakerName}
        onClose={() => setSettingsOpen(false)}
      />
    </Shell>
  );
}

export default function HomePage() {
  // UI language: starts from the browser, then the speaker's saved preference
  // (GET /api/settings) overrides once loaded; changing it persists via PUT.
  const [lang, setLang] = useState(detectInitialLang);
  const t = useMemo(() => makeT(lang), [lang]);

  // Colour scheme: 'system' | 'light' | 'dark', persisted per device. The hook
  // applies the resolved theme to <html> and follows the OS in 'system' mode.
  const [themeMode, setThemeMode] = useThemeMode();

  const [search, setSearch] = useState(null); // null | { mode:'browse' } | { mode:'assign', slot:N }
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [speakerName, setSpeakerName] = useState('SoundTouch');
  const [speakerModel, setSpeakerModel] = useState('');
  const [settingsLoaded, setSettingsLoaded] = useState(false);

  const data = useSpeaker();

  // Load saved language + speaker name from the speaker once. Track when this
  // resolves so the player can keep its skeleton until the real name is known —
  // otherwise it briefly shows the "SoundTouch" default and then jumps.
  useEffect(() => {
    getSettings()
      .then((s) => {
        if (!s) return;
        if (s.language && LANGS.some((l) => l.code === s.language)) setLang(s.language);
        if (clean(s.name)) setSpeakerName(clean(s.name));
        if (clean(s.model)) setSpeakerModel(clean(s.model));
      })
      .finally(() => setSettingsLoaded(true));
  }, []);

  // Reflect the speaker's name in the browser tab, e.g. "Living Room · ReTouch",
  // falling back to plain "ReTouch" before the name loads. The iOS home-screen
  // shortcut takes its label from apple-mobile-web-app-title (read when the user
  // taps "Add to Home Screen"), so set that to the bare radio name — a pinned
  // tile then reads e.g. "Keuken" instead of "ReTouch".
  useEffect(() => {
    const nm = clean(speakerName);
    document.title = nm ? `${nm} · ReTouch` : 'ReTouch';
    const meta = document.querySelector('meta[name="apple-mobile-web-app-title"]');
    if (meta) meta.setAttribute('content', nm || 'ReTouch');
  }, [speakerName]);

  const handleSetLang = useCallback((code) => {
    setLang(code);
    saveSettings({ language: code });
  }, []);

  return (
    <I18nContext.Provider value={{ t, lang }}>
      <HomeBody
        lang={lang}
        onSetLang={handleSetLang}
        themeMode={themeMode}
        onSetTheme={setThemeMode}
        speakerName={speakerName}
        setSpeakerName={setSpeakerName}
        speakerModel={speakerModel}
        search={search}
        setSearch={setSearch}
        settingsOpen={settingsOpen}
        setSettingsOpen={setSettingsOpen}
        data={data}
        settingsLoaded={settingsLoaded}
      />
    </I18nContext.Provider>
  );
}
