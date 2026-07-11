import styled from 'styled-components';
import { FieldCard } from '../../../molecules/Field';

// --- qr field: a scannable pairing code plus its typeable digits -----------
// Used by plugins like Apple Home. The field carries { value } (the payload the QR
// encodes) and an optional { code } (the human-readable version, tap to copy).
export const QrCardEl = styled(FieldCard)`
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 20px 18px 16px;
`;

export const QrQuiet = styled.div`
  padding: 12px;
  background: #fff;
  border-radius: 14px;
  box-shadow: 0 0 0 1px rgba(31, 24, 20, 0.06);
  line-height: 0;
`;

export const QrCodeBtn = styled.button`
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 18px;
  padding: 8px 10px 8px 16px;
  background: var(--surface-2);
  border-radius: 12px;
  -webkit-tap-highlight-color: transparent;
`;

export const QrCodeText = styled.span`
  font-size: 21px;
  font-weight: 700;
  letter-spacing: 0.1em;
  font-variant-numeric: tabular-nums;
  color: var(--ink);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
`;

export const QrCopy = styled.span`
  display: flex;
  align-items: center;
  gap: 5px;
  flex-shrink: 0;
  padding: 6px 10px;
  border-radius: 9px;
  background: #fff;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--accent);
`;

// --- installed-plugin header: a tappable row that expands the plugin's settings.
export const PlugHead = styled.button`
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 14px 0;
  text-align: left;
  -webkit-tap-highlight-color: transparent;
`;

export const PlugName = styled.div`
  font-size: 14.5px;
  font-weight: 600;
  color: var(--ink-2);
`;

export const PlugMeta = styled.div`
  font-size: 12px;
  color: var(--ink-3);
  margin-top: 2px;
`;

// A small "!" badge shown on an installed-plugin row when an over-the-air update
// is available — so the version itself stays hidden but a pending update is
// glanceable without opening the plugin.
export const UpdateBadge = styled.span`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 18px;
  height: 18px;
  border-radius: 99px;
  background: #f1c40f;
  color: #1a1a1a;
  font-size: 12px;
  font-weight: 700;
  line-height: 1;
`;

// The "Instellingen ›" affordance on an installed-plugin row.
export const PlugAction = styled.span`
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--accent);
`;
