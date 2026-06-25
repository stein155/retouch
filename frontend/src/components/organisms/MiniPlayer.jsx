import styled from 'styled-components';
import { StationLogo } from '../atoms/StationLogo';
import { VolumeSlider } from '../molecules/VolumeSlider';
import { Icon } from '../atoms/Icon';
import { LiveDot } from '../atoms/LiveDot';
import { useI18n } from '../../lib/i18n';
import { mpfade } from '../../theme/keyframes';

const clean = (value) => (typeof value === 'string' ? value.trim() : '');

const Mp = styled.div`
  position: absolute;
  left: 14px;
  right: 14px;
  bottom: max(14px, calc(env(safe-area-inset-bottom) + 8px));
  z-index: 20;
  flex-shrink: 0;
  background: #fff;
  border-radius: 22px;
  padding: 12px;
  transition: transform 360ms cubic-bezier(.34,.78,.18,1), box-shadow 300ms ease;
  box-shadow:
    0 1px 0 rgba(255, 255, 255, 0.8) inset,
    0 18px 40px -16px rgba(31, 24, 20, 0.22),
    0 0 0 1px rgba(31, 24, 20, 0.05);

  ${(p) => p.$on && `background: linear-gradient(180deg, var(--accent-soft), #fff);`}

  @media (prefers-reduced-motion: reduce) { transition: none; }
`;

const MpIdle = styled.div`
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 4px;
  color: var(--ink-2);
  font-size: 13px;
  font-weight: 500;
`;

const MpIdleBlob = styled.span`
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--surface-3);
  display: grid;
  place-items: center;
  color: var(--ink-3);
  flex-shrink: 0;
`;

const MpIdleText = styled.span`
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
`;

const MpIdleVol = styled.span`
  font-size: 11.5px;
  color: var(--ink-3);
  font-weight: 500;
`;

const MpTop = styled.div`
  display: flex;
  align-items: center;
  gap: 12px;
`;

const MpArt = styled.div`
  position: relative;
  width: 52px;
  height: 52px;
  border-radius: 14px;
  flex-shrink: 0;
`;

const MpArtInner = styled.div`
  width: 100%;
  height: 100%;
  border-radius: inherit;
  display: grid;
  place-items: center;
  color: var(--ink);
  background: var(--surface-3);
  box-shadow: inset 0 0 0 1px rgba(31, 24, 20, 0.05);
  padding: 0 4px;

  &:has(img) {
    background: transparent;
    box-shadow: 0 1px 4px rgba(31, 24, 20, 0.10), 0 0 0 1px rgba(31, 24, 20, 0.06);
    padding: 0;
    overflow: hidden;
  }
`;

const MpLivePill = styled.span`
  position: absolute;
  bottom: -6px;
  left: 50%;
  transform: translateX(-50%);
  background: #fff;
  color: var(--accent);
  font-size: 9px;
  font-weight: 800;
  letter-spacing: 0.08em;
  padding: 2px 6px;
  border-radius: 99px;
  box-shadow: 0 2px 6px -2px rgba(31, 24, 20, 0.18), 0 0 0 1px rgba(31, 24, 20, 0.06);
  display: inline-flex;
  align-items: center;
  gap: 3px;
  white-space: nowrap;
`;

const MpMeta = styled.div`
  flex: 1;
  min-width: 0;
`;

const MpName = styled.div`
  font-size: 15.5px;
  font-weight: 700;
  letter-spacing: -0.02em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  animation: ${mpfade} 320ms cubic-bezier(.34,.78,.18,1);

  @media (prefers-reduced-motion: reduce) { animation: none; }
`;

const MpSub = styled.div`
  font-size: 12px;
  color: var(--ink-2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  margin-top: 2px;
  font-weight: 500;
`;

const MpStop = styled.button`
  width: 42px;
  height: 42px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: var(--accent);
  color: #fff;
  flex-shrink: 0;
  transition: transform 180ms ease, box-shadow 180ms ease;
  box-shadow: 0 6px 14px -4px rgba(196, 122, 82, 0.55);

  &:hover { transform: scale(1.06); }
  &:active { transform: scale(0.96); }

  @media (prefers-reduced-motion: reduce) { transition: none; }
`;

const MpVol = styled.div`
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 12px;
  padding: 0 2px;
`;

const MpVolIcon = styled.button`
  color: var(--ink-2);
  flex-shrink: 0;
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border-radius: 8px;

  &:hover { background: var(--surface-3); color: var(--ink); }
`;

const MpVolVal = styled.span`
  width: 30px;
  text-align: right;
  font-size: 12px;
  color: var(--ink-2);
  flex-shrink: 0;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
`;

export function MiniPlayer({ player, volume, speakerName, onStop, onVolume }) {
  const { t } = useI18n();
  const { status, station } = player;
  const active = status !== 'idle' && !!station;
  const playing = status === 'playing';
  const displayName = clean(station?.name) || clean(station?.track) || '?';
  const speaker = clean(speakerName) || 'SoundTouch';
  const muted = volume === 0;

  // Current track line from TuneIn now-playing: "Artist · Title" when both are
  // known, else whichever we have. Hidden when it would just repeat the station.
  const artist = clean(station?.artist);
  const track = clean(station?.track);
  const nowLine = artist && track ? `${artist} · ${track}` : track || artist;
  const showNow = playing && nowLine && nowLine !== displayName;

  // Pill + sub-line label tracks the start-up phase so the user sees progress
  // instead of the station blinking away.
  const statusLabel =
    status === 'buffering' ? t('buffering') : status === 'starting' ? t('starting') : t('live');

  return (
    <Mp $on={active}>
      {active ? (
        <>
          <MpTop>
            <MpArt>
              <MpArtInner>
                <StationLogo name={displayName} tuneInId={station?.tuneInId} logo={station?.art} />
              </MpArtInner>
              {playing && (
                <MpLivePill>
                  <LiveDot />{statusLabel}
                </MpLivePill>
              )}
            </MpArt>
            <MpMeta>
              <MpName key={displayName}>{displayName}</MpName>
              <MpSub>
                {playing
                  ? `${showNow ? `${nowLine} · ` : ''}${t('on')} ${speaker}`
                  : `${statusLabel} · ${speaker}`}
              </MpSub>
            </MpMeta>
            <MpStop aria-label={t('stop')} onClick={onStop}>
              <Icon.stop width="18" height="18" />
            </MpStop>
          </MpTop>
          <MpVol>
            <MpVolIcon
              onClick={() => onVolume(muted ? 25 : 0)}
              aria-label={t('mute')}
            >
              {muted
                ? <Icon.mute width="18" height="18" />
                : <Icon.volume width="18" height="18" />
              }
            </MpVolIcon>
            <VolumeSlider value={volume} onChange={onVolume} />
            <MpVolVal>{volume}</MpVolVal>
          </MpVol>
        </>
      ) : (
        <MpIdle>
          <MpIdleBlob>
            <Icon.play width="12" height="12" />
          </MpIdleBlob>
          <MpIdleText>
            {t('chooseStationFor')} <b style={{ color: 'var(--ink)' }}>{speaker}</b>
          </MpIdleText>
          <MpIdleVol>{t('vol')} {volume}</MpIdleVol>
        </MpIdle>
      )}
    </Mp>
  );
}

export default MiniPlayer;
