import { describe, it, expect, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { renderWithTheme, screen } from '../../../test/render';
import type { Player, Preset } from '../../../lib/types';
import { PresetTile } from '.';

const idle = { status: 'idle', station: null } as Player;
const playingFoo = { status: 'playing', station: { name: 'Radio Foo', tuneInId: 's1' } } as Player;
const startingFoo = { status: 'starting', station: { name: 'Radio Foo', tuneInId: 's1' } } as Player;

describe('PresetTile', () => {
  it('renders an empty slot that calls onAssign when tapped', async () => {
    const onAssign = vi.fn();
    renderWithTheme(
      <PresetTile preset={null} player={idle} onPlay={() => {}} onAssign={onAssign} />,
    );
    // Empty-slot copy (English) is shown.
    expect(screen.getByText('Empty slot')).toBeInTheDocument();
    expect(screen.getByText('tap to fill')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button'));
    expect(onAssign).toHaveBeenCalledTimes(1);
  });

  it('renders a filled tile and calls onPlay on its main button', async () => {
    const onPlay = vi.fn();
    const preset = { name: 'Radio Foo', tuneInId: 's1', logo: '' } as Preset;
    renderWithTheme(
      <PresetTile preset={preset} player={idle} onPlay={onPlay} onAssign={() => {}} />,
    );
    expect(screen.getByText('Radio Foo')).toBeInTheDocument();
    // First button is the play (tile-main) target.
    await userEvent.click(screen.getAllByRole('button')[0]);
    expect(onPlay).toHaveBeenCalledTimes(1);
  });

  it('shows the live equalizer when active and playing', () => {
    const preset = { name: 'Radio Foo', tuneInId: 's1', logo: '' } as Preset;
    const { container } = renderWithTheme(
      <PresetTile preset={preset} player={playingFoo} active onPlay={() => {}} onAssign={() => {}} />,
    );
    // Equalizer = four animated <i> bars; the starting spinner has none.
    expect(container.querySelectorAll('i').length).toBe(4);
  });

  it('shows a starting spinner (not the equalizer) when active and starting', () => {
    const preset = { name: 'Radio Foo', tuneInId: 's1', logo: '' } as Preset;
    const { container } = renderWithTheme(
      <PresetTile preset={preset} player={startingFoo} active onPlay={() => {}} onAssign={() => {}} />,
    );
    expect(container.querySelectorAll('i').length).toBe(0);
  });

  it('does not highlight when not the active tile, even if a station plays', () => {
    const preset = { name: 'Radio Bar', tuneInId: 's2', logo: '' } as Preset;
    const { container } = renderWithTheme(
      <PresetTile preset={preset} player={playingFoo} active={false} onPlay={() => {}} onAssign={() => {}} />,
    );
    expect(container.querySelectorAll('i').length).toBe(0);
  });

  it('opens the replace menu from the more button', async () => {
    const onAssign = vi.fn();
    const preset = { name: 'Radio Foo', tuneInId: 's1', logo: '' } as Preset;
    renderWithTheme(
      <PresetTile preset={preset} player={idle} onPlay={() => {}} onAssign={onAssign} />,
    );
    // The more button is rendered in the test's default language (English).
    await userEvent.click(screen.getByRole('button', { name: 'Replace' }));
    const replace = screen.getByText('Replace');
    expect(replace).toBeInTheDocument();
    await userEvent.click(replace);
    expect(onAssign).toHaveBeenCalledTimes(1);
  });
});
