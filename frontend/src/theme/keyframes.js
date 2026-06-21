import { keyframes } from 'styled-components';

// Shared keyframes, copied verbatim from styles.css. styled-components scopes
// keyframes to a generated name, so they are imported where used.

export const livepulse = keyframes`
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.45; transform: scale(0.8); }
`;

export const bars = keyframes`
  0%, 100% { transform: scaleY(0.4); transform-origin: bottom; }
  50% { transform: scaleY(1); transform-origin: bottom; }
`;

export const spin = keyframes`
  to { transform: rotate(360deg); }
`;

export const pop = keyframes`
  from { opacity: 0; transform: translateY(-4px) scale(0.96); }
  to   { opacity: 1; transform: translateY(0) scale(1); }
`;

export const mpfade = keyframes`
  from { opacity: 0; transform: translateY(3px); }
  to   { opacity: 1; transform: none; }
`;
