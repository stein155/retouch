import type * as React from 'react';
import { useLayoutEffect, useRef, useState } from 'react';
import { Viewport, Content } from './styled';

type Props = {
  text: React.ReactNode;
  className?: string;
};

// Marquee shows text on one line: it fits, it's ellipsised, or — when it would
// clip — it gently scrolls end to end so the whole thing is readable. Motion is
// only enabled when the text actually overflows and the user hasn't asked for
// reduced motion; otherwise it degrades to a plain ellipsis.
export function Marquee({ text, className }: Props) {
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const contentRef = useRef<HTMLSpanElement | null>(null);
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
        style={
          scrolling
            ? ({ '--mq-shift': `${shift}px`, '--mq-dur': `${dur}s` } as React.CSSProperties)
            : undefined
        }
      >
        {text}
      </Content>
    </Viewport>
  );
}

export default Marquee;
