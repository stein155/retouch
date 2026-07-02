import styled from 'styled-components';
import { shimmer } from '../../theme/keyframes';

// A shimmering placeholder block shown while real content loads. Size it with
// width/height (or let a parent size it); $radius overrides the corner radius.
// Reduced-motion users get a static tint instead of the sweep.
export const Skeleton = styled.span`
  display: block;
  background-color: var(--surface-3);
  background-image: linear-gradient(
    90deg,
    transparent 0,
    rgba(255, 255, 255, 0.55) 50%,
    transparent 100%
  );
  background-size: 150% 100%;
  background-repeat: no-repeat;
  border-radius: ${(p) => p.$radius || '9px'};
  animation: ${shimmer} 1.4s ease-in-out infinite;

  @media (prefers-reduced-motion: reduce) {
    animation: none;
    background-image: none;
  }
`;

export default Skeleton;
