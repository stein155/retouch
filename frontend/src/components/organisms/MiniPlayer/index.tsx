import { useEffect, useRef } from 'react';
import { StationLogo } from '../../atoms/StationLogo';
import { VolumeSlider } from '../../molecules/VolumeSlider';
import { Icon } from '../../atoms/Icon';
import { LiveDot } from '../../atoms/LiveDot';
import { Skeleton } from '../../atoms/Skeleton';
import { useI18n } from '../../../lib/i18n';
import {
  Mp, MpIdle, MpIdleBlob, MpIdleText, MpIdleVol, MpTop, MpArt, MpArtInner,
  MpLivePill, MpMeta, MpName, MpSub, MpStop, MpVol, MpVolIcon, MpVolVal,
} from './styled';
import type { Player } from '../../../lib/types';

const clean = (value: unknown): string => (typeof value === 'string' ? value.trim() : '');

// Skeleton shown while the first now-playing / volume read is in flight, so the
// player doesn't flash its idle "choose a station" state before we know if
// something is already playing.
function MiniPlayerSkeleton() {
  return (
    <Mp aria-hidden="true">
      <MpTop>
        <MpArt>
          <Skeleton style={{ width: '100%', height: '100%' }} $radius="14px" />
        </MpArt>
        <MpMeta>
          <Skeleton style={{ width: '60%', height: 15 }} $radius="6px" />
          <Skeleton style={{ width: '40%', height: 12, marginTop: 8 }} $radius="6px" />
        </MpMeta>
        <Skeleton style={{ width: 42, height: 42, flexShrink: 0 }} $radius="50%" />
      </MpTop>
      <MpVol>
        <Skeleton style={{ width: 28, height: 28, flexShrink: 0 }} $radius="8px" />
        <Skeleton style={{ flex: 1, height: 8 }} $radius="99px" />
        <Skeleton style={{ width: 30, height: 12, flexShrink: 0 }} $radius="6px" />
      </MpVol>
    </Mp>
  );
}

interface Props {
  player: Player;
  volume: number;
  speakerName: string;
  loading: boolean;
  onStop: () => void;
  onVolume: (v: number) => void;
}

export function MiniPlayer({ player, volume, speakerName, loading, onStop, onVolume }: Props) {
  const { t } = useI18n();
  const { status, station } = player;

  // Remember the level before muting so unmute restores it instead of jumping to
  // a hard-coded default. Tracks the last non-zero volume however it was reached
  // (mute button or slider dragged to 0). Declared before the loading early-return
  // so the hook order stays stable.
  const preMute = useRef<number>(volume || 25);
  useEffect(() => { if (volume > 0) preMute.current = volume; }, [volume]);

  if (loading) return <MiniPlayerSkeleton />;

  const active = status !== 'idle' && !!station;
  const playing = status === 'playing';
  const displayName = clean(station?.name) || clean(station?.track) || '?';
  const speaker = clean(speakerName) || 'SoundTouch';
  const muted = volume === 0;
  const toggleMute = () => onVolume(muted ? (preMute.current > 0 ? preMute.current : 25) : 0);

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
              <MpName key={displayName} text={displayName} />
              <MpSub
                text={
                  playing
                    ? `${showNow ? `${nowLine} · ` : ''}${t('on')} ${speaker}`
                    : `${statusLabel} · ${speaker}`
                }
              />
            </MpMeta>
            <MpStop aria-label={t('stop')} onClick={onStop}>
              <Icon.stop width="18" height="18" />
            </MpStop>
          </MpTop>
          <MpVol>
            <MpVolIcon
              onClick={toggleMute}
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
