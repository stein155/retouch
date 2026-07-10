import { useState, useRef, useEffect } from 'react';
import { StationLogo } from '../../atoms/StationLogo';
import { Icon } from '../../atoms/Icon';
import { EqualizerBars } from '../../atoms/EqualizerBars';
import { Spinner } from '../../atoms/Spinner';
import { Skeleton } from '../../atoms/Skeleton';
import { useI18n } from '../../../lib/i18n';
import type { Preset, Player } from '../../../lib/types';
import {
  TileMore,
  TileLogo,
  TileName,
  Tile,
  TileMain,
  TileRow,
  TileLive,
  TileEmpty,
  TileEmptyMid,
  TileEmptyLabel,
  TileEmptyHint,
  Popover,
  TileSkeleton,
} from './styled';

const clean = (value: unknown): string => (typeof value === 'string' ? value.trim() : '');

interface Props {
  preset: Preset | null;
  player: Player;
  active?: boolean;
  onPlay: () => void;
  onAssign: () => void;
}

export function PresetTileSkeleton() {
  return (
    <TileSkeleton aria-hidden="true">
      <Skeleton style={{ width: 44, height: 44 }} $radius="12px" />
      <Skeleton style={{ width: '70%', height: 16 }} $radius="6px" />
    </TileSkeleton>
  );
}

export function PresetTile({ preset, player, active, onPlay, onAssign }: Props) {
  const { t } = useI18n();
  const [menu, setMenu] = useState(false);
  const ref = useRef<HTMLDivElement | null>(null);

  // Close the menu on an outside click.
  useEffect(() => {
    if (!menu) return;
    const fn = (e: MouseEvent | TouchEvent) => { if (ref.current && !ref.current.contains(e.target as Node)) setMenu(false); };
    document.addEventListener('mousedown', fn);
    document.addEventListener('touchstart', fn);
    return () => {
      document.removeEventListener('mousedown', fn);
      document.removeEventListener('touchstart', fn);
    };
  }, [menu]);

  if (!preset) {
    return (
      <TileEmpty onClick={onAssign}>
        <TileEmptyMid>
          <Icon.plus width="22" height="22" />
        </TileEmptyMid>
        <span>
          <TileEmptyLabel>{t('emptySlot')}</TileEmptyLabel>
          <TileEmptyHint>{t('tapToFill')}</TileEmptyHint>
        </span>
      </TileEmpty>
    );
  }

  const name = clean(preset.name) || '?';

  // Whether this is the preset the player is on is resolved once by the grid (see
  // PresetGrid / activePresetIndex) and passed in as `active`, so a loose name
  // match can't light up several tiles at once. starting/buffering shows a loader
  // on the tile; PLAY_STATE shows the live equalizer.
  const isPlaying = active && player.status === 'playing';
  const isStarting = active && (player.status === 'starting' || player.status === 'buffering');

  return (
    <Tile $playing={isPlaying} $starting={isStarting} ref={ref}>
      <TileMain onClick={onPlay}>
        <TileRow>
          <TileLogo>
            <StationLogo name={name} tuneInId={preset.tuneInId} logo={preset.logo} />
          </TileLogo>
        </TileRow>
        <span>
          <TileName>{name}</TileName>
        </span>
        {isPlaying && (
          <TileLive>
            <EqualizerBars />
          </TileLive>
        )}
        {isStarting && (
          <TileLive>
            <Spinner $tile />
          </TileLive>
        )}
      </TileMain>
      <TileMore
        aria-label={t('replace')}
        onClick={(e) => { e.stopPropagation(); setMenu(m => !m); }}
      >
        <Icon.more width="16" height="16" />
      </TileMore>
      {menu && (
        <Popover role="menu">
          <button onClick={() => { setMenu(false); onAssign(); }}>{t('replace')}</button>
        </Popover>
      )}
    </Tile>
  );
}

export default PresetTile;
