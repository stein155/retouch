import styled from 'styled-components';

// Slider primitives, copied verbatim from the .slider* rules in styles.css.
// SliderRoot carries the :active thumb-scale behaviour. $bass applies the
// .bass-slider height override.

export const SliderRoot = styled.div<{ $bass?: boolean }>`
  position: relative;
  flex: 1;
  height: 24px;
  cursor: pointer;
  touch-action: none;
  display: flex;
  align-items: center;

  &:active .slider-thumb { transform: translate(-50%, -50%) scale(1.18); }

  ${(p) => p.$bass && `height: 26px;`}
`;

export const SliderTrack = styled.div`
  position: absolute;
  left: 0;
  right: 0;
  top: 50%;
  height: 6px;
  transform: translateY(-50%);
  background: var(--surface-3);
  border-radius: 99px;
`;

export const SliderFill = styled.div`
  position: absolute;
  left: 0;
  top: 50%;
  height: 6px;
  transform: translateY(-50%);
  background: var(--accent);
  border-radius: 99px;
`;

// Keeps the .slider-thumb class so the SliderRoot :active selector still hits it.
export const SliderThumb = styled.div.attrs({ className: 'slider-thumb' })`
  position: absolute;
  top: 50%;
  width: 20px;
  height: 20px;
  border-radius: 99px;
  background: #fff;
  box-shadow:
    0 2px 8px -1px rgba(31, 24, 20, 0.18),
    0 0 0 1px rgba(31, 24, 20, 0.08);
  transform: translate(-50%, -50%);
  transition: transform 120ms ease;
`;

// .bass-center — the centre origin tick on the bass slider.
export const SliderCenter = styled.div`
  position: absolute;
  top: 50%;
  width: 2px;
  height: 14px;
  transform: translate(-50%, -50%);
  background: var(--border-strong);
  border-radius: 99px;
`;
