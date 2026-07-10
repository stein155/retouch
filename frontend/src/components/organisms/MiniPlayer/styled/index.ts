import styled from 'styled-components';
import { Marquee } from '../../../atoms/Marquee';
import { mpfade } from '../../../../theme/keyframes';

export const Mp = styled.div`
  position: absolute;
  left: 14px;
  right: 14px;
  bottom: max(14px, calc(env(safe-area-inset-bottom) + 8px));
  z-index: 20;
  flex-shrink: 0;
  background: var(--surface);
  border-radius: 22px;
  padding: 12px;
  transition: transform 360ms cubic-bezier(.34,.78,.18,1), box-shadow 300ms ease;
  box-shadow:
    0 1px 0 rgba(255, 255, 255, 0.8) inset,
    0 18px 40px -16px rgba(31, 24, 20, 0.22),
    0 0 0 1px rgba(31, 24, 20, 0.05);

  ${(p) => p.$on && `background: linear-gradient(180deg, var(--accent-soft), var(--surface));`}

  @media (prefers-reduced-motion: reduce) { transition: none; }
`;

export const MpIdle = styled.div`
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 4px;
  color: var(--ink-2);
  font-size: 13px;
  font-weight: 500;
`;

export const MpIdleBlob = styled.span`
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--surface-3);
  display: grid;
  place-items: center;
  color: var(--ink-3);
  flex-shrink: 0;
`;

export const MpIdleText = styled.span`
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
`;

export const MpIdleVol = styled.span`
  font-size: 11.5px;
  color: var(--ink-3);
  font-weight: 500;
`;

export const MpTop = styled.div`
  display: flex;
  align-items: center;
  gap: 12px;
`;

export const MpArt = styled.div`
  position: relative;
  width: 52px;
  height: 52px;
  border-radius: 14px;
  flex-shrink: 0;
`;

export const MpArtInner = styled.div`
  width: 100%;
  height: 100%;
  border-radius: inherit;
  display: grid;
  place-items: center;
  color: var(--ink);
  background: var(--surface-3);
  box-shadow: inset 0 0 0 1px rgba(31, 24, 20, 0.05);
  padding: 0 4px;

  &:has(img) {
    background: transparent;
    box-shadow: 0 1px 4px rgba(31, 24, 20, 0.10), 0 0 0 1px rgba(31, 24, 20, 0.06);
    padding: 0;
    overflow: hidden;
  }
`;

export const MpLivePill = styled.span`
  position: absolute;
  bottom: -6px;
  left: 50%;
  transform: translateX(-50%);
  background: #fff;
  color: var(--accent);
  font-size: 9px;
  font-weight: 800;
  letter-spacing: 0.08em;
  padding: 2px 6px;
  border-radius: 99px;
  box-shadow: 0 2px 6px -2px rgba(31, 24, 20, 0.18), 0 0 0 1px rgba(31, 24, 20, 0.06);
  display: inline-flex;
  align-items: center;
  gap: 3px;
  white-space: nowrap;
`;

export const MpMeta = styled.div`
  flex: 1;
  min-width: 0;
`;

export const MpName = styled(Marquee)`
  font-size: 15.5px;
  font-weight: 700;
  letter-spacing: -0.02em;
  animation: ${mpfade} 320ms cubic-bezier(.34,.78,.18,1);

  @media (prefers-reduced-motion: reduce) { animation: none; }
`;

export const MpSub = styled(Marquee)`
  font-size: 12px;
  color: var(--ink-2);
  margin-top: 2px;
  font-weight: 500;
`;

export const MpStop = styled.button`
  width: 42px;
  height: 42px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: var(--accent);
  color: #fff;
  flex-shrink: 0;
  transition: transform 180ms ease, box-shadow 180ms ease;
  box-shadow: 0 6px 14px -4px rgba(196, 122, 82, 0.55);

  &:hover { transform: scale(1.06); }
  &:active { transform: scale(0.96); }

  @media (prefers-reduced-motion: reduce) { transition: none; }
`;

export const MpVol = styled.div`
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 12px;
  padding: 0 2px;
`;

export const MpVolIcon = styled.button`
  color: var(--ink-2);
  flex-shrink: 0;
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border-radius: 8px;

  &:hover { background: var(--surface-3); color: var(--ink); }
`;

export const MpVolVal = styled.span`
  width: 30px;
  text-align: right;
  font-size: 12px;
  color: var(--ink-2);
  flex-shrink: 0;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
`;
