import { describe, it, expect } from 'vitest';
import { renderWithTheme, screen } from '../../../test/render';
import { StationLogo } from '.';

describe('StationLogo', () => {
  it('shows two-letter initials when there is no logo or id', () => {
    const { container } = renderWithTheme(<StationLogo name="Jazz FM" />);
    expect(screen.getByText('JF')).toBeInTheDocument();
    expect(container.querySelector('img')).toBeNull();
  });

  it('renders a proxied <img> for an explicit logo url', () => {
    const { container } = renderWithTheme(<StationLogo name="Jazz FM" logo="http://cdn/x.png" />);
    const img = container.querySelector('img');
    expect(img).not.toBeNull();
    expect(img?.getAttribute('src')).toBe(`/api/logo?u=${encodeURIComponent('http://cdn/x.png')}`);
  });

  it('derives a TuneIn CDN logo from the station id', () => {
    const { container } = renderWithTheme(<StationLogo name="X" tuneInId="s42" />);
    const src = container.querySelector('img')?.getAttribute('src') || '';
    expect(src).toContain('/api/logo?u=');
    expect(decodeURIComponent(src)).toContain('cdn-radiotime-logos.tunein.com/s42g.png');
  });
});
