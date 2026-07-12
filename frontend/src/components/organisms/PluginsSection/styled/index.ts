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

// Stays white in dark mode too: a QR code needs a light quiet zone to scan.
export const QrQuiet = styled.div`
  padding: 12px;
  background: #fff;
  border-radius: 14px;
  box-shadow: 0 0 0 1px var(--border);
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
  background: var(--surface);
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

// --- manifest panel: the server-driven settings UI ---------------------------

// The coloured status line at the top of a plugin panel, as a quiet pill so it
// reads as state rather than another hint line.
export const PanelStatus = styled.div`
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border-radius: 12px;
  background: var(--surface-2);
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-2);
  line-height: 1.35;
`;

export const StatusDot = styled.span<{ $color: string }>`
  flex-shrink: 0;
  width: 8px;
  height: 8px;
  border-radius: 99px;
  background: ${(p) => p.$color};
`;

// One manifest section: eyebrow title + optional hint + card + actions.
export const PanelSection = styled.div`
  margin-top: 18px;
`;

// A manifest action button. $kind maps the manifest's style field:
// primary = filled accent, danger = quiet red, default = surface.
export const ActionBtn = styled.button<{ $kind?: string }>`
  margin-top: 10px;
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 9px;
  border: none;
  border-radius: 14px;
  padding: 14px 16px;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
  transition: transform 160ms ease, opacity 160ms ease;

  background: var(--surface);
  color: var(--ink-2);
  box-shadow: var(--shadow-sm);

  ${(p) => p.$kind === 'primary' && `
    background: var(--accent);
    color: #fff;
    font-weight: 700;
    box-shadow: 0 8px 18px -6px color-mix(in oklab, var(--accent) 60%, transparent);
  `}
  ${(p) => p.$kind === 'danger' && `
    background: transparent;
    color: #d44030;
    box-shadow: none;
  `}

  &:active:not(:disabled) { transform: scale(0.99); }
  &:disabled { opacity: 0.38; cursor: default; }
`;

// The panel's field card: like FieldCard, but any two adjacent rows divide —
// FieldRow's own \`& + &\` rule only covers FieldRow pairs, and the manifest can
// interleave plain rows with slider/times rows in any order.
export const PanelCard = styled(FieldCard)`
  & > * + * { border-top: 1px solid var(--border); }
`;

// --- slider field: label + live value on one line, track beneath -------------
export const SliderRow = styled.div`
  padding: 13px 0 15px;
`;

export const SliderHead = styled.div`
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
`;

export const SliderVal = styled.span`
  font-size: 15px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: var(--accent);
`;

// --- times field: a list of HH:MM chips plus an add affordance ---------------
export const TimesRow = styled.div`
  padding: 13px 0;
`;

export const TimeChips = styled.div`
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
`;

// One scheduled time. The whole chip is the remove button: time + ×.
export const TimeChip = styled.button`
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 8px 12px;
  border: none;
  border-radius: 99px;
  background: var(--accent-soft);
  color: var(--ink);
  font-size: 14px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;

  & svg { color: var(--ink-3); }
`;

export const TimeAdd = styled.button`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border: 1px dashed var(--border-strong);
  border-radius: 99px;
  background: transparent;
  color: var(--accent);
  font-size: 13.5px;
  font-weight: 600;
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
`;

// --- add-time modal -----------------------------------------------------------
export const ModalScrim = styled.div`
  position: fixed;
  inset: 0;
  z-index: 60;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgba(0, 0, 0, 0.35);
  backdrop-filter: blur(2px);
`;

export const ModalCard = styled.div`
  width: 100%;
  max-width: 300px;
  background: var(--surface);
  border-radius: var(--radius);
  padding: 20px 18px 14px;
  box-shadow: var(--shadow-lg);
`;

export const ModalTitle = styled.div`
  font-size: 15px;
  font-weight: 700;
  color: var(--ink);
  text-align: center;
`;

// The native time input, sized up so the picker is comfortably tappable.
export const ModalTimeInput = styled.input`
  display: block;
  width: 100%;
  margin: 16px 0 6px;
  padding: 12px;
  border: none;
  border-radius: 12px;
  background: var(--surface-2);
  color: var(--ink);
  font-size: 22px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  text-align: center;
  outline: none;
  color-scheme: light dark;
`;

export const ModalBtns = styled.div`
  display: flex;
  gap: 8px;
  margin-top: 10px;

  & > * { flex: 1; margin-top: 0; }
`;
