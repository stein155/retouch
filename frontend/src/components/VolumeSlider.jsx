import { useRef, useState, useEffect } from 'react';

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
    <div
      ref={ref}
      className="slider"
      onMouseDown={(e) => { setDragging(true); update(e.clientX); }}
      onTouchStart={(e) => { setDragging(true); update(e.touches[0].clientX); }}
    >
      <div className="slider-track" />
      <div className="slider-fill" style={{ width: `${value}%` }} />
      <div className="slider-thumb" style={{ left: `${value}%` }} />
    </div>
  );
}
