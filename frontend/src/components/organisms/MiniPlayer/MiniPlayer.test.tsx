import { describe, it, expect, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { renderWithTheme, screen } from '../../../test/render';
import { MiniPlayer } from '.';
import type { Player } from '../../../lib/types';

const playing: Player = {
  status: 'playing',
  station: { name: 'Jazz FM', art: '', tuneInId: 's1', track: 'Song', artist: 'Artist' },
};

function setup(volume: number) {
  const onVolume = vi.fn();
  const onStop = vi.fn();
  const utils = renderWithTheme(
    <MiniPlayer player={playing} volume={volume} speakerName="Keuken" loading={false} onStop={onStop} onVolume={onVolume} />,
  );
  return { onVolume, onStop, ...utils };
}

describe('MiniPlayer', () => {
  it('shows the station name and the live track line', () => {
    setup(40);
    expect(screen.getByText('Jazz FM')).toBeInTheDocument();
    expect(screen.getByText(/Artist · Song/)).toBeInTheDocument();
  });

  it('does not repeat the track when the station name is unknown', () => {
    // With no real station name, line one falls back to the track; the track line
    // below is suppressed so the same text isn't shown twice.
    const noName: Player = {
      status: 'playing',
      station: { name: '', art: '', tuneInId: null, track: 'Song', artist: 'Artist' },
    };
    renderWithTheme(
      <MiniPlayer player={noName} volume={20} speakerName="Keuken" loading={false} onStop={() => {}} onVolume={() => {}} />,
    );
    expect(screen.getByText('Song')).toBeInTheDocument();
    expect(screen.queryByText(/Artist · Song/)).not.toBeInTheDocument();
  });

  it('mutes to 0 and restores the previous volume on unmute', async () => {
    const user = userEvent.setup();
    const { onVolume, rerender } = setup(60);

    // At volume 60, tapping the mute icon drops to 0.
    await user.click(screen.getByRole('button', { name: 'Mute' }));
    expect(onVolume).toHaveBeenLastCalledWith(0);

    // Parent applied the mute -> re-render at 0; unmute must restore 60, not a default.
    rerender(
      <MiniPlayer player={playing} volume={0} speakerName="Keuken" loading={false} onStop={() => {}} onVolume={onVolume} />,
    );
    await user.click(screen.getByRole('button', { name: 'Mute' }));
    expect(onVolume).toHaveBeenLastCalledWith(60);
  });

  it('renders a skeleton while loading (no controls)', () => {
    renderWithTheme(
      <MiniPlayer player={playing} volume={20} speakerName="Keuken" loading onStop={() => {}} onVolume={() => {}} />,
    );
    expect(screen.queryByRole('button', { name: 'Mute' })).not.toBeInTheDocument();
  });
});
