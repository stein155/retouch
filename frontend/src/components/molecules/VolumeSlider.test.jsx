import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent } from '@testing-library/react';
import { renderWithTheme } from '../../test/render';
import { VolumeSlider } from './VolumeSlider';

// The slider maps a pointer's clientX within its box to a 0..100 value. jsdom
// reports a zero-size box, so we stub getBoundingClientRect to a known geometry.
function stubRect(el, { left = 0, width = 200 } = {}) {
  el.getBoundingClientRect = () => ({ left, width, top: 0, height: 24, right: left + width, bottom: 24 });
}

describe('VolumeSlider', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('reports the clicked value via onChange (mousedown)', () => {
    const onChange = vi.fn();
    const { container } = renderWithTheme(<VolumeSlider value={20} onChange={onChange} />);
    const root = container.firstChild;
    stubRect(root, { left: 0, width: 200 });

    // Click at 150px of a 200px-wide track => 75%.
    fireEvent.mouseDown(root, { clientX: 150 });
    expect(onChange).toHaveBeenCalledWith(75);
  });

  it('clamps below 0 and above 100', () => {
    const onChange = vi.fn();
    const { container } = renderWithTheme(<VolumeSlider value={50} onChange={onChange} />);
    const root = container.firstChild;
    stubRect(root, { left: 0, width: 200 });

    fireEvent.mouseDown(root, { clientX: -40 });
    expect(onChange).toHaveBeenLastCalledWith(0);

    fireEvent.mouseDown(root, { clientX: 999 });
    expect(onChange).toHaveBeenLastCalledWith(100);
  });

  it('continues to update while dragging after mousedown', () => {
    const onChange = vi.fn();
    const { container } = renderWithTheme(<VolumeSlider value={0} onChange={onChange} />);
    const root = container.firstChild;
    stubRect(root, { left: 0, width: 200 });

    fireEvent.mouseDown(root, { clientX: 20 }); // 10%
    fireEvent.mouseMove(window, { clientX: 100 }); // 50%
    expect(onChange).toHaveBeenLastCalledWith(50);
  });
});
