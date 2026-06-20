import { useState, useCallback, useMemo, useEffect } from 'react';
import { useSpeaker } from './hooks/useSpeaker';
import { Presets } from './components/Presets';
import { MiniPlayer } from './components/MiniPlayer';
import { SearchSheet } from './components/SearchSheet';
import { SettingsSheet } from './components/SettingsSheet';
import { Icon } from './components/Icons';
import { playPreset, stopPlayback, setVolume, storePreset, selectStation, getSettings, saveSettings } from './lib/api';
import { makeT, I18nContext, useI18n, detectInitialLang, LANGS } from './lib/i18n';

const clean = (value) => (typeof value === 'string' ? value.trim() : '');

// Single speaker driven through the on-box STLocal agent (runs on the speaker
// itself and serves this UI at /). The speaker name comes from its own settings.
function Header({ onSettings }) {
  const { t } = useI18n();
  return (
    <header className="hdr">
      <div className="hdr-brand">
        <span className="hdr-blob" aria-hidden="true">📻</span>
        <span className="hdr-title">{t('appTitle')}</span>
      </div>
      <button className="hdr-gear" onClick={onSettings} aria-label={t('settings')}>
        <Icon.settings width="22" height="22" />
      </button>
    </header>
  );
}

export default function App() {
  // UI language: starts from the browser, then the speaker's saved preference
  // (GET /api/settings) overrides once loaded; changing it persists via PUT.
  const [lang, setLang] = useState(detectInitialLang);
  const t = useMemo(() => makeT(lang), [lang]);

  const [search, setSearch] = useState(null); // null | { mode:'browse' } | { mode:'assign', slot:N }
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [speakerName, setSpeakerName] = useState('SoundTouch');
  const [speakerModel, setSpeakerModel] = useState('');

  const data = useSpeaker();

  // Load saved language + speaker name from the speaker once.
  useEffect(() => {
    getSettings().then((s) => {
      if (!s) return;
      if (s.language && LANGS.some((l) => l.code === s.language)) setLang(s.language);
      if (clean(s.name)) setSpeakerName(clean(s.name));
      if (clean(s.model)) setSpeakerModel(clean(s.model));
    });
  }, []);

  const handleSetLang = useCallback((code) => {
    setLang(code);
    saveSettings({ language: code });
  }, []);

  const handlePlay = useCallback(async (preset, slot) => {
    const standby = data.player.status !== 'playing';
    data.playOptimistic(preset); // show it instantly
    data.nudge();
    await playPreset(slot, standby, preset);
    data.refreshNowPlaying();
  }, [data]);

  const handleStop = useCallback(async () => {
    data.stopOptimistic(); // collapse to idle right away
    await stopPlayback();
    setTimeout(data.refreshNowPlaying, 1000);
  }, [data]);

  const handleVolume = useCallback(async (v) => {
    data.setVolumeOptimistic(v);
    await setVolume(v);
  }, [data]);

  const handleAssign = useCallback((slot) => setSearch({ mode: 'assign', slot }), []);

  const handlePick = useCallback(async (station) => {
    // Stations come straight from live TuneIn search, so the id is always
    // current (no hardcoded catalog ids that could go stale).
    if (!station.tuneInId) { setSearch(null); return; }
    if (search?.mode === 'assign') {
      await storePreset(search.slot, station.tuneInId, station.name, station.logo);
      await data.refreshPresets();
    }
    data.playOptimistic(station); // show it instantly
    data.nudge();
    setSearch(null);
    await selectStation(station.tuneInId, station.name);
    data.refreshNowPlaying();
  }, [search, data]);

  return (
    <I18nContext.Provider value={{ t, lang }}>
    <div className="shell">
      <Header onSettings={() => setSettingsOpen(true)} />

      <div className="shell-scroll">
        <Presets
          presets={data.presets}
          player={data.player}
          onPlay={handlePlay}
          onAssign={handleAssign}
        />

        <section className="sect">
          <button className="browse" onClick={() => setSearch({ mode: 'browse' })}>
            <span className="browse-icon"><Icon.search width="20" height="20" /></span>
            <span className="browse-text">
              <span className="browse-title">{t('findNew')}</span>
              <span className="browse-sub">{t('findNewSub')}</span>
            </span>
            <span className="browse-cta">→</span>
          </button>
        </section>

        <footer className="ftr">
          <span>{t('connectedWith')}</span>
          <span className="ftr-bose">{speakerModel ? `Bose ${speakerModel}` : 'Bose'}</span>
        </footer>
      </div>

      <MiniPlayer
        player={data.player}
        volume={data.volume}
        speakerName={speakerName}
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
        onSetLang={handleSetLang}
        onNameChange={setSpeakerName}
        onClose={() => setSettingsOpen(false)}
      />
    </div>
    </I18nContext.Provider>
  );
}
