import styled from 'styled-components';
import { livepulse } from '../../theme/keyframes';

// .live-dot — pulsing dot used in the LIVE indicators.
export const LiveDot = styled.span`
  width: 6px;
  height: 6px;
  border-radius: 99px;
  background: currentColor;
  animation: ${livepulse} 1.6s ease-in-out infinite;

  @media (prefers-reduced-motion: reduce) {
    animation: none;
  }
`;

export default LiveDot;
