import styled from 'styled-components';
import { bars } from '../../theme/keyframes';

// .bars — animated equalizer shown on the playing preset tile.
const Bars = styled.span`
  display: inline-flex;
  align-items: end;
  gap: 3px;
  height: 14px;

  i {
    display: block;
    width: 3px;
    background: var(--accent);
    border-radius: 2px;
    animation: ${bars} 900ms ease-in-out infinite;
  }
  i:nth-child(1) { height: 60%; animation-delay: 0ms; }
  i:nth-child(2) { height: 100%; animation-delay: 150ms; }
  i:nth-child(3) { height: 40%; animation-delay: 300ms; }
  i:nth-child(4) { height: 80%; animation-delay: 450ms; }

  @media (prefers-reduced-motion: reduce) {
    i { animation: none; }
  }
`;

export function EqualizerBars(props) {
  return (
    <Bars {...props}>
      <i /><i /><i /><i />
    </Bars>
  );
}

export default EqualizerBars;
