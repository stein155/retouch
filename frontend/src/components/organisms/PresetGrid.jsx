import { useMemo } from 'react';
import styled from 'styled-components';
import { Section } from '../atoms/Section';
import { PresetTile, PresetTileSkeleton } from '../molecules/PresetTile';
import { useI18n } from '../../lib/i18n';
import { activePresetIndex } from '../../lib/station';

const SectHead = styled.div`
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 14px;
`;

const SectTitle = styled.span`
  font-size: 18px;
  font-weight: 700;
  letter-spacing: -0.02em;
`;

const SectSub = styled.span`
  font-size: 12px;
  color: var(--ink-2);
`;

const Grid = styled.div`
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
`;

export function PresetGrid({ presets, player, loading, onPlay, onAssign }) {
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
