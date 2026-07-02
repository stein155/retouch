import styled from 'styled-components';
import { Section } from '../atoms/Section';
import { PresetTile } from '../molecules/PresetTile';
import { Skeleton } from '../atoms/Skeleton';
import { useI18n } from '../../lib/i18n';

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

// Placeholder tile matching the real tile's footprint (same aspect-ratio and
// min-height), so the grid holds its shape while presets load instead of
// flashing empty "tap to fill" slots and jumping when the stations arrive.
const SkeletonTile = styled.div`
  background: #fff;
  border-radius: var(--radius);
  aspect-ratio: 1.1 / 1;
  min-height: 116px;
  box-shadow: var(--shadow-sm);
  padding: 14px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
`;

export function PresetGrid({ presets, player, loading, onPlay, onAssign }) {
  const { t } = useI18n();
  return (
    <Section>
      <SectHead>
        <SectTitle>{t('yourStations')}</SectTitle>
        <SectSub>{t('tapToPlay')}</SectSub>
      </SectHead>
      <Grid>
        {loading
          ? Array.from({ length: 6 }, (_, i) => (
            <SkeletonTile key={i} aria-hidden="true">
              <Skeleton $w="44px" $h="44px" $r="12px" />
              <Skeleton $w="70%" $h="16px" />
            </SkeletonTile>
          ))
          : presets.map((preset, i) => (
            <PresetTile
              key={i}
              preset={preset}
              player={player}
              onPlay={() => preset && onPlay(preset, i + 1)}
              onAssign={() => onAssign(i + 1)}
            />
          ))}
      </Grid>
    </Section>
  );
}

export default PresetGrid;
