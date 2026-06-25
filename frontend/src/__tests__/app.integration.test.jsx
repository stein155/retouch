import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// Mock the whole API client: no real network, deterministic data.
vi.mock('../lib/api');
import * as api from '../lib/api';

import App from '../App';

const PRESETS = [
  { slot: 1, name: 'Radio Foo', tuneInId: 's1', location: '', logo: '' },
  { slot: 2, name: 'Radio Bar', tuneInId: 's2', location: '', logo: '' },
  null, null, null, null,
];

function stubRect(el, { left = 0, width = 200 } = {}) {
  el.getBoundingClientRect = () => ({ left, width, top: 0, height: 24, right: left + width, bottom: 24 });
}

beforeEach(() => {
  vi.clearAllMocks();
  // useSpeaker reads these on mount + poll.
  api.getNowPlaying.mockResolvedValue({ standby: true });
  api.getVolume.mockResolvedValue(30);
  api.getPresets.mockResolvedValue(PRESETS);
  // HomePage + SettingsSheet read settings.
  api.getSettings.mockResolvedValue({ name: 'Kitchen', model: 'ST20', language: 'en', host: 'kitchen.local', bass: { actual: 0, min: -9, max: 0, default: 0 } });
  api.getVersion.mockResolvedValue({ version: '1.2.3', updatable: false });
  // SettingsSheet also fetches beta builds on open.
  api.getReleases.mockResolvedValue({ current: '1.2.3', updatable: false, stable: null, betas: [] });
  api.findSpeakers.mockResolvedValue([]);
  api.saveSettings.mockResolvedValue(null);
  api.playPreset.mockResolvedValue(null);
  api.setVolume.mockResolvedValue(null);
  api.stopPlayback.mockResolvedValue(null);
  api.selectStation.mockResolvedValue(null);
  api.storePreset.mockResolvedValue(null);
  api.searchTuneIn.mockResolvedValue([
    { tuneInId: 's9', name: 'Jazz Live', tagline: 'smooth', genre: '', country: 'NL', logo: '' },
  ]);
});

describe('App integration', () => {
  it('renders presets from the mocked API', async () => {
    render(<App />);
    expect(await screen.findByText('Radio Foo')).toBeInTheDocument();
    expect(screen.getByText('Radio Bar')).toBeInTheDocument();
    // Empty slots show the empty-slot copy.
    expect((await screen.findAllByText('Empty slot')).length).toBe(4);
  });

  it('plays a preset when its tile is clicked', async () => {
    render(<App />);
    const foo = await screen.findByText('Radio Foo');
    await userEvent.click(foo);
    await waitFor(() => expect(api.playPreset).toHaveBeenCalledTimes(1));
    // Called with the slot number (1) for Radio Foo.
    expect(api.playPreset.mock.calls[0][0]).toBe(1);
  });

  it('sets the volume when the mini-player slider changes', async () => {
    // Speaker is playing so the mini-player shows its volume slider.
    api.getNowPlaying.mockResolvedValue({
      standby: false, playStatus: 'PLAY_STATE', stationName: 'Radio Foo',
      tuneInId: 's1', track: '', artist: '', art: '',
    });
    render(<App />);
    await screen.findAllByText('Radio Foo');

    // The volume value (30) is shown once active; the slider precedes it.
    const volVal = await screen.findByText('30');
    const sliderRoot = volVal.previousElementSibling; // SliderRoot
    stubRect(sliderRoot, { left: 0, width: 200 });
    fireEvent.mouseDown(sliderRoot, { clientX: 100 }); // 50%
    await waitFor(() => expect(api.setVolume).toHaveBeenCalledWith(50));
  });

  it('opens the search sheet from the browse row', async () => {
    render(<App />);
    await screen.findByText('Radio Foo');
    await userEvent.click(screen.getByText('Find something new'));
    // The discover headline appears in the sheet.
    expect(await screen.findByText('Discover stations')).toBeInTheDocument();
    // Typing a query runs the (mocked) TuneIn search and lists a result.
    const input = screen.getByPlaceholderText('Stations, genres, countries…');
    await userEvent.type(input, 'jazz');
    expect(await screen.findByText('Jazz Live')).toBeInTheDocument();
  });

  it('opens the settings sheet and persists a language change', async () => {
    render(<App />);
    await screen.findByText('Radio Foo');
    await userEvent.click(screen.getByRole('button', { name: 'Settings' }));
    // Settings headline + version (from getVersion) appear.
    expect(await screen.findByText('Settings')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText('1.2.3')).toBeInTheDocument());

    // Change language -> persists via saveSettings.
    const select = screen.getByLabelText('Language');
    await userEvent.selectOptions(select, 'de');
    expect(api.saveSettings).toHaveBeenCalledWith({ language: 'de' });
  });
});
