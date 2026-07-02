import { useLayoutEffect, useRef, useState } from 'react';
import styled, { css, keyframes } from 'styled-components';

// Scroll the text left, pause, scroll back — the ease + the pauses at each end
// (12% of the cycle) make a full long title readable without a frantic loop.
// The shift distance and duration are fed in per-instance via CSS variables.
const scroll = keyframes`
  0%, 12%   { transform: translateX(0); }
  88%, 100% { transform: translateX(calc(-1 * var(--mq-shift))); }
`;

const Viewport = styled.div`
  overflow: hidden;
`;

const Content = styled.span`
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

// Marquee shows text on one line: it fits, it's ellipsised, or — when it would
// clip — it gently scrolls end to end so the whole thing is readable. Motion is
// only enabled when the text actually overflows and the user hasn't asked for
// reduced motion; otherwise it degrades to a plain ellipsis.
export function Marquee({ text, className }) {
  const viewportRef = useRef(null);
  const contentRef = useRef(null);
  const [shift, setShift] = useState(0);

  // useLayoutEffect so the measurement (and any resulting scroll) is applied
  // before paint, avoiding a flash of clipped-then-scrolling text.
  useLayoutEffect(() => {
    const vp = viewportRef.current;
    const ct = contentRef.current;
    if (!vp || !ct) return undefined;

    const mq = window.matchMedia?.('(prefers-reduced-motion: reduce)');
    const reduce = mq ? mq.matches : false;

    const measure = () => {
      // Full text width minus the visible width; >4px means it's clipped.
      const overflow = ct.scrollWidth - vp.clientWidth;
      setShift(!reduce && overflow > 4 ? overflow : 0);
    };
    measure();

    if (typeof ResizeObserver === 'undefined') return undefined;
    const ro = new ResizeObserver(measure);
    ro.observe(vp);
    return () => ro.disconnect();
  }, [text]);

  const scrolling = shift > 0;
  // ~45px/s of travel, floored so short overflows still ease rather than dart.
  const dur = Math.max(4, shift / 45 + 1.5);

  return (
    <Viewport ref={viewportRef} className={className}>
      <Content
        ref={contentRef}
        $scroll={scrolling}
        style={scrolling ? { '--mq-shift': `${shift}px`, '--mq-dur': `${dur}s` } : undefined}
      >
        {text}
      </Content>
    </Viewport>
  );
}

export default Marquee;
