import { StationLogo } from './StationLogo';
import { VolumeSlider } from './VolumeSlider';
import { Icon } from './Icons';
import { useI18n } from '../lib/i18n';

const cx = (...a) => a.filter(Boolean).join(' ');
const clean = (value) => (typeof value === 'string' ? value.trim() : '');

export function MiniPlayer({ player, volume, speakerName, onStop, onVolume }) {
  const { t } = useI18n();
  const { status, station } = player;
  const active = status !== 'idle' && !!station;
  const playing = status === 'playing';
  const displayName = clean(station?.name) || clean(station?.track) || '?';
  const speaker = clean(speakerName) || 'SoundTouch';
  const muted = volume === 0;

  // Pill + sub-line label tracks the start-up phase so the user sees progress
  // instead of the station blinking away.
  const statusLabel =
    status === 'buffering' ? t('buffering') : status === 'starting' ? t('starting') : t('live');

  return (
    <div className={cx('mp', active && 'mp-on', `mp-${status}`)}>
      {active ? (
        <>
          <div className="mp-top">
            <div className="mp-art">
              <div className="mp-art-inner">
                <StationLogo name={displayName} tuneInId={station?.tuneInId} logo={station?.art} />
              </div>
              {playing && (
                <span className="mp-live-pill">
                  <span className="live-dot" />{statusLabel}
                </span>
              )}
            </div>
            <div className="mp-meta">
              <div className="mp-name" key={displayName}>{displayName}</div>
              <div className="mp-sub">
                {playing
                  ? `${clean(station?.track) && clean(station.track) !== displayName ? `${clean(station.track)} · ` : ''}${t('on')} ${speaker}`
                  : `${statusLabel} · ${speaker}`}
              </div>
            </div>
            <button className="mp-stop" aria-label={t('stop')} onClick={onStop}>
              <Icon.stop width="18" height="18" />
            </button>
          </div>
          <div className="mp-vol">
            <button
              className="mp-vol-icon"
              onClick={() => onVolume(muted ? 25 : 0)}
              aria-label={t('mute')}
            >
              {muted
                ? <Icon.mute width="18" height="18" />
                : <Icon.volume width="18" height="18" />
              }
            </button>
            <VolumeSlider value={volume} onChange={onVolume} />
            <span className="mp-vol-val">{volume}</span>
          </div>
        </>
      ) : (
        <div className="mp-idle">
          <span className="mp-idle-blob">
            <Icon.play width="12" height="12" />
          </span>
          <span className="mp-idle-text">
            {t('chooseStationFor')} <b style={{ color: 'var(--ink)' }}>{speaker}</b>
          </span>
          <span className="mp-idle-vol">{t('vol')} {volume}</span>
        </div>
      )}
    </div>
  );
}
