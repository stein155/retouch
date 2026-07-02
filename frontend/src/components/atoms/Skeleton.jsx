import styled from 'styled-components';
import { shimmer } from '../../theme/keyframes';

// A shimmering placeholder block, sized by width/height props. Used to stand in
// for content while the settings sheet loads its data. Reduced-motion users get
// a static tinted block instead of the sweep.
export const Skeleton = styled.span`
  display: block;
  width: ${(p) => p.$w || '100%'};
  height: ${(p) => p.$h || '15px'};
  border-radius: ${(p) => p.$r || '7px'};
  background: linear-gradient(
    100deg,
    rgba(31, 24, 20, 0.06) 30%,
    rgba(31, 24, 20, 0.12) 50%,
    rgba(31, 24, 20, 0.06) 70%
  );
  background-size: 200% 100%;
  animation: ${shimmer} 1.3s ease-in-out infinite;

  @media (prefers-reduced-motion: reduce) {
    animation: none;
    background: rgba(31, 24, 20, 0.08);
  }
`;

export default Skeleton;
