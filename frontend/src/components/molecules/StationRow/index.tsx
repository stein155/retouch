import { StationLogo } from '../../atoms/StationLogo';
import { Icon } from '../../atoms/Icon';
import type { Station } from '../../../lib/types';
import { Row, RowPlay, RowArt, RowText, RowName, RowSub, RowDot, RowTagline } from './styled';

interface Props {
  // The component defensively reads a legacy `id` alongside the canonical
  // `tuneInId`, so allow it here without dropping the shared Station shape.
  station: Station & { id?: string | null };
  onPick: () => void;
}

export function StationRow({ station, onPick }: Props) {
  const id = station.id || null;
  return (
    <Row onClick={onPick}>
      <RowArt>
        <StationLogo id={id} name={station.name} tuneInId={station.tuneInId} logo={station.logo} />
      </RowArt>
      <RowText>
        <RowName>{station.name}</RowName>
        <RowSub>
          <span>{station.genre}</span>
          {station.tagline && <><RowDot>·</RowDot><RowTagline>{station.tagline}</RowTagline></>}
          {station.country && <><RowDot>·</RowDot><span>{station.country}</span></>}
        </RowSub>
      </RowText>
      <RowPlay><Icon.play width="12" height="12" /></RowPlay>
    </Row>
  );
}

export default StationRow;
