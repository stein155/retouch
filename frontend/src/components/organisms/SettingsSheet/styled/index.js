import styled from 'styled-components';
import { Button } from '../../../atoms/Button';

// .spk-scan — multiroom scan button: the dark variant of .update-btn. Its label
// is --bg (not #fff) so it inverts with the theme: dark button + light label in
// light mode, light button + dark label in dark mode.
export const ScanButton = styled(Button).attrs({ $variant: 'update' })`
  background: var(--ink);
  color: var(--bg);
  &:hover { background: var(--ink-2); }
`;

// Shown once an update lands: a blocking full-screen overlay that forces the
// page to reload onto the new bundle. Sits above the sheet (z-index 50) so the
// stale app can't be used in the meantime.
export const UpdateOverlay = styled.div`
  position: fixed;
  inset: 0;
  z-index: 60;
  background: rgba(31, 24, 20, 0.45);
  backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
`;

export const UpdateCard = styled.div`
  background: var(--surface);
  border-radius: 22px;
  padding: 30px 26px;
  max-width: 320px;
  width: 100%;
  text-align: center;
  box-shadow: 0 30px 80px -20px rgba(31, 24, 20, 0.35);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
`;

export const UpdateTitle = styled.div`
  font-size: 17px;
  font-weight: 700;
  color: var(--ink);
`;

export const UpdateSub = styled.div`
  font-size: 14px;
  color: var(--ink-3);
`;

// Root settings menu: one tappable row per category, each opening its own
// subpage. Styled to sit on the same card as the form fields it replaces.
export const MenuItem = styled.button`
  display: flex;
  align-items: center;
  gap: 14px;
  width: 100%;
  padding: 14px 0;
  text-align: left;
  background: transparent;
  border: none;
  cursor: pointer;
  color: var(--ink);

  & + & { border-top: 1px solid rgba(31, 24, 20, 0.07); }
`;

export const MenuIcon = styled.span`
  width: 34px;
  height: 34px;
  border-radius: 11px;
  background: var(--accent-soft);
  color: var(--accent);
  display: grid;
  place-items: center;
  flex-shrink: 0;
`;

export const MenuLabel = styled.span`
  flex: 1;
  min-width: 0;
  font-size: 15px;
  font-weight: 600;
`;

export const MenuChev = styled.span`
  color: var(--ink-3);
  display: grid;
  place-items: center;
  flex-shrink: 0;
`;
