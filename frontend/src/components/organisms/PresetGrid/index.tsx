import { useMemo } from 'react';
import { Section } from '../../atoms/Section';
import { PresetTile, PresetTileSkeleton } from '../../molecules/PresetTile';
import { useI18n } from '../../../lib/i18n';
import { activePresetIndex } from '../../../lib/station';
import { SectHead, SectTitle, SectSub, Grid } from './styled';
import type { Preset, Player } from '../../../lib/types';

interface Props {
  presets: (Preset | null)[];
  player: Player;
  loading: boolean;
  onPlay: (preset: Preset, slot: number) => void;
  onAssign: (slot: number) => void;
}

export function PresetGrid({ presets, player, loading, onPlay, onAssign }: Props) {
  const { t } = useI18n();
  // Resolve the one active slot here, not per-tile: a loose name match could
  // otherwise light up several tiles at once (see activePresetIndex).
  const activeIdx = useMemo(
    () => activePresetIndex(presets, player.station),
    [presets, player.station],
  );
  return (
    <Section>
      <SectHead>
        <SectTitle>{t('yourStations')}</SectTitle>
        <SectSub>{t('tapToPlay')}</SectSub>
      </SectHead>
      <Grid>
        {loading
          ? Array.from({ length: 6 }, (_, i) => <PresetTileSkeleton key={i} />)
          : presets.map((preset, i) => (
            <PresetTile
              key={i}
              preset={preset}
              player={player}
              active={i === activeIdx}
              onPlay={() => preset && onPlay(preset, i + 1)}
              onAssign={() => onAssign(i + 1)}
            />
          ))}
      </Grid>
    </Section>
  );
}

export default PresetGrid;
