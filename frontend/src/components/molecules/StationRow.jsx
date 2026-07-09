import styled from 'styled-components';
import { StationLogo } from '../atoms/StationLogo';
import { Icon } from '../atoms/Icon';

// .row-play — declared first so Row can reveal it on hover.
const RowPlay = styled.div`
  width: 32px;
  height: 32px;
  border-radius: 99px;
  display: grid;
  place-items: center;
  color: #fff;
  background: var(--accent);
  flex-shrink: 0;
  opacity: 0;
  transition: opacity 160ms ease, transform 160ms ease;

  @media (hover: none) { opacity: 1; }
`;

const Row = styled.button`
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 10px 8px;
  border-radius: 14px;
  transition: background 160ms ease;
  width: 100%;
  text-align: left;

  &:hover { background: var(--surface-2); }
  &:hover ${RowPlay} { opacity: 1; transform: scale(1.05); }
`;

const RowArt = styled.div`
  width: 44px;
  height: 44px;
  border-radius: 12px;
  display: grid;
  place-items: center;
  background: var(--surface-3);
  color: var(--ink);
  flex-shrink: 0;
  overflow: hidden;
  padding: 0;

  &:has(img) {
    background: transparent;
    box-shadow: 0 1px 4px rgba(31, 24, 20, 0.10), 0 0 0 1px rgba(31, 24, 20, 0.06);
  }
`;

const RowText = styled.div`
  flex: 1;
  min-width: 0;
`;

const RowName = styled.div`
  font-size: 14.5px;
  font-weight: 700;
  letter-spacing: -0.015em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
`;

const RowSub = styled.div`
  font-size: 12px;
  color: var(--ink-2);
  display: flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  margin-top: 2px;
  font-weight: 500;
`;

const RowDot = styled.span`
  opacity: 0.4;
`;

const RowTagline = styled.span`
  overflow: hidden;
  text-overflow: ellipsis;
`;

export function StationRow({ station, onPick }) {
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
