import { useState, useEffect, useRef } from 'react';
import { SliderRoot, SliderTrack, SliderFill, SliderThumb, SliderCenter } from '../../atoms/Slider';

// Centre-origin bass slider over the speaker's real capability range (e.g. -9..0).
// The "origin" tick + fill anchor at the default (or 0 if in range).
export function BassSlider({ value, min, max, origin, onChange }) {
  const ref = useRef(null);
  const [dragging, setDragging] = useState(false);
  const span = max - min || 1;

  const update = (clientX) => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const ratio = Math.max(0, Math.min(1, (clientX - r.left) / r.width));
    onChange(Math.round(min + ratio * span));
  };

  useEffect(() => {
    if (!dragging) return;
    const move = (e) => update(e.touches ? e.touches[0].clientX : e.clientX);
    const up = () => setDragging(false);
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    window.addEventListener('touchmove', move, { passive: false });
    window.addEventListener('touchend', up);
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
      window.removeEventListener('touchmove', move);
      window.removeEventListener('touchend', up);
    };
  }, [dragging]);

  const pct = ((value - min) / span) * 100;
  const originPct = Math.max(0, Math.min(100, ((origin - min) / span) * 100));
  const fillLeft = Math.min(originPct, pct);
  const fillWidth = Math.abs(pct - originPct);

  return (
    <SliderRoot
      ref={ref}
      $bass
      role="slider"
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={value}
      tabIndex={0}
      onMouseDown={(e) => { setDragging(true); update(e.clientX); }}
      onTouchStart={(e) => { setDragging(true); update(e.touches[0].clientX); }}
      onKeyDown={(e) => {
        if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') { e.preventDefault(); onChange(Math.max(min, value - 1)); }
        if (e.key === 'ArrowRight' || e.key === 'ArrowUp') { e.preventDefault(); onChange(Math.min(max, value + 1)); }
      }}
    >
      <SliderTrack />
      <SliderCenter style={{ left: `${originPct}%` }} />
      <SliderFill style={{ left: `${fillLeft}%`, width: `${fillWidth}%` }} />
      <SliderThumb style={{ left: `${pct}%` }} />
    </SliderRoot>
  );
}

export default BassSlider;
