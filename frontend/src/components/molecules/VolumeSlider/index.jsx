import { useRef, useState, useEffect } from 'react';
import { SliderRoot, SliderTrack, SliderFill, SliderThumb } from '../../atoms/Slider';

export function VolumeSlider({ value, onChange }) {
  const ref = useRef(null);
  const [dragging, setDragging] = useState(false);

  const update = (clientX) => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const pct = Math.max(0, Math.min(100, Math.round(((clientX - r.left) / r.width) * 100)));
    onChange(pct);
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

  return (
    <SliderRoot
      ref={ref}
      onMouseDown={(e) => { setDragging(true); update(e.clientX); }}
      onTouchStart={(e) => { setDragging(true); update(e.touches[0].clientX); }}
    >
      <SliderTrack />
      <SliderFill style={{ width: `${value}%` }} />
      <SliderThumb style={{ left: `${value}%` }} />
    </SliderRoot>
  );
}

export default VolumeSlider;
