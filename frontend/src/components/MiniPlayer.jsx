import { StationLogo } from './StationLogo';
import { VolumeSlider } from './VolumeSlider';
import { Icon } from './Icons';
import { useI18n } from '../lib/i18n';

const cx = (...a) => a.filter(Boolean).join(' ');

export function MiniPlayer({ nowPlaying, volume, speakerName, onStop, onVolume }) {
  const { t } = useI18n();
  const playing = nowPlaying && !nowPlaying.standby && nowPlaying.playStatus === 'PLAY_STATE';
  const displayName = nowPlaying?.stationName || '';
  const muted = volume === 0;

  return (
    <div className={cx('mp', playing && 'mp-on')}>
      {playing ? (
        <>
          <div className="mp-top">
            <div className="mp-art">
              <div className="mp-art-inner">
                <StationLogo name={displayName} tuneInId={nowPlaying?.tuneInId} logo={nowPlaying?.art} />
              </div>
              <span className="mp-live-pill">
                <span className="live-dot" />{t('live')}
              </span>
            </div>
            <div className="mp-meta">
              <div className="mp-name">{displayName}</div>
              <div className="mp-sub">
                {nowPlaying?.track && nowPlaying.track !== displayName
                  ? `${nowPlaying.track} · `
                  : ''}
                {t('on')} {speakerName}
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
            {t('chooseStationFor')} <b style={{ color: 'var(--ink)' }}>{speakerName}</b>
          </span>
          <span className="mp-idle-vol">{t('vol')} {volume}</span>
        </div>
      )}
    </div>
  );
}
