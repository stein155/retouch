import styled from 'styled-components';
import { spin } from '../../theme/keyframes';

// .mp-spinner — loading spinner shown while a station is starting / buffering.
// $tile applies the smaller .tile-spinner override; $scan applies the white
// .spk-scan .mp-spinner override used inside the multiroom scan button.
export const Spinner = styled.span`
  width: 9px;
  height: 9px;
  border-radius: 50%;
  border: 1.5px solid rgba(196, 122, 82, 0.3);
  border-top-color: var(--accent);
  animation: ${spin} 700ms linear infinite;

  ${(p) => p.$tile && `
    width: 13px;
    height: 13px;
    border-width: 2px;
  `}

  ${(p) => p.$scan && `
    width: 15px;
    height: 15px;
    border-width: 2px;
    border-color: rgba(255, 255, 255, 0.4);
    border-top-color: #fff;
  `}

  @media (prefers-reduced-motion: reduce) {
    animation: none;
  }
`;

export default Spinner;
