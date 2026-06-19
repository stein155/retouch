import { useState, useRef, useEffect } from 'react';
import { StationLogo } from './StationLogo';
import { Icon } from './Icons';
import { useI18n } from '../lib/i18n';
import { sameStation } from '../lib/station';

const cx = (...a) => a.filter(Boolean).join(' ');

function PresetTile({ preset, player, onPlay, onAssign }) {
  const { t } = useI18n();
  const [menu, setMenu] = useState(false);
  const ref = useRef(null);

  // Close the menu on an outside click.
  useEffect(() => {
    if (!menu) return;
    const fn = (e) => { if (ref.current && !ref.current.contains(e.target)) setMenu(false); };
    document.addEventListener('mousedown', fn);
    document.addEventListener('touchstart', fn);
    return () => {
      document.removeEventListener('mousedown', fn);
      document.removeEventListener('touchstart', fn);
    };
  }, [menu]);

  if (!preset) {
    return (
      <button className="tile tile-empty" onClick={onAssign}>
        <span className="tile-empty-mid">
          <Icon.plus width="22" height="22" />
        </span>
        <span>
          <span className="tile-empty-label">{t('emptySlot')}</span>
          <span className="tile-empty-hint" style={{ display: 'block' }}>{t('tapToFill')}</span>
        </span>
      </button>
    );
  }

  const name = preset.name || '?';

  // Is this the preset the player is on? Match by TuneIn id when known, else by
  // name (the speaker doesn't expose the live id). starting/buffering shows a
  // loader on the tile; PLAY_STATE shows the live equalizer.
  const st = player.station;
  const matches = !!st && (
    (preset.tuneInId && st.tuneInId && preset.tuneInId === st.tuneInId) ||
    sameStation(preset.name, st.name)
  );
  const isPlaying = matches && player.status === 'playing';
  const isStarting = matches && (player.status === 'starting' || player.status === 'buffering');

  return (
    <div className={cx('tile', isPlaying && 'is-playing', isStarting && 'is-starting')} ref={ref}>
      <button className="tile-main" onClick={onPlay}>
        <span className="tile-row">
          <span className="tile-logo">
            <StationLogo name={name} tuneInId={preset.tuneInId} logo={preset.logo} />
          </span>
        </span>
        <span>
          <span className="tile-name">{name}</span>
        </span>
      {isPlaying && (
        <span className="tile-live">
          <span className="bars"><i/><i/><i/><i/></span>
        </span>
      )}
      {isStarting && (
        <span className="tile-live">
          <span className="mp-spinner tile-spinner" />
        </span>
      )}
      </button>
      <button
        className="tile-more"
        aria-label="Bewerken"
        onClick={(e) => { e.stopPropagation(); setMenu(m => !m); }}
      >
        <Icon.more width="16" height="16" />
      </button>
      {menu && (
        <div className="popover" role="menu">
          <button onClick={() => { setMenu(false); onAssign(); }}>{t('replace')}</button>
        </div>
      )}
    </div>
  );
}

export function Presets({ presets, player, onPlay, onAssign }) {
  const { t } = useI18n();
  return (
    <section className="sect">
      <div className="sect-head">
        <span className="sect-title">{t('yourStations')}</span>
        <span className="sect-sub">{t('tapToPlay')}</span>
      </div>
      <div className="preset-grid">
        {presets.map((preset, i) => (
          <PresetTile
            key={i}
            preset={preset}
            player={player}
            onPlay={() => preset && onPlay(preset, i + 1)}
            onAssign={() => onAssign(i + 1)}
          />
        ))}
      </div>
    </section>
  );
}
