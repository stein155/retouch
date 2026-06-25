import styled from 'styled-components';
import { Section } from '../atoms/Section';
import { PresetTile } from '../molecules/PresetTile';
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

export function PresetGrid({ presets, player, onPlay, onAssign }) {
  const { t } = useI18n();
  return (
    <Section>
      <SectHead>
        <SectTitle>{t('yourStations')}</SectTitle>
        <SectSub>{t('tapToPlay')}</SectSub>
      </SectHead>
      <Grid>
        {presets.map((preset, i) => (
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
