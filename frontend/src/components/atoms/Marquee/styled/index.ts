import styled, { css, keyframes } from 'styled-components';

// Scroll the text left, pause, scroll back — the ease + the pauses at each end
// (12% of the cycle) make a full long title readable without a frantic loop.
// The shift distance and duration are fed in per-instance via CSS variables.
const scroll = keyframes`
  0%, 12%   { transform: translateX(0); }
  88%, 100% { transform: translateX(calc(-1 * var(--mq-shift))); }
`;

export const Viewport = styled.div`
  overflow: hidden;
`;

export const Content = styled.span<{ $scroll?: boolean }>`
  white-space: nowrap;
  ${(p) =>
    p.$scroll
      ? css`
          display: inline-block;
          will-change: transform;
          animation: ${scroll} var(--mq-dur) ease-in-out infinite alternate;
        `
      : css`
          display: block;
          overflow: hidden;
          text-overflow: ellipsis;
        `}

  @media (prefers-reduced-motion: reduce) {
    animation: none;
  }
`;
