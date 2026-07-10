import styled from 'styled-components';

// Shared bottom-sheet shell used by the search and settings sheets. All rules
// copied verbatim from the .sheet* block in styles.css, including the desktop
// (min-width: 720px) centred-dialog behaviour. $open maps to .is-open.

export const SheetScrim = styled.div<{ $open?: boolean }>`
  position: fixed;
  inset: 0;
  background: rgba(31, 24, 20, 0.25);
  opacity: 0;
  pointer-events: none;
  transition: opacity 280ms ease;
  z-index: 40;
  backdrop-filter: blur(6px);

  ${(p) => p.$open && `opacity: 1; pointer-events: auto;`}
`;

export const SheetEl = styled.div<{ $open?: boolean }>`
  position: fixed;
  left: 50%;
  bottom: 0;
  transform: translate(-50%, 100%);
  width: 100%;
  max-width: var(--shell-max);
  height: 88dvh;
  background: var(--bg);
  border-top-left-radius: 28px;
  border-top-right-radius: 28px;
  display: flex;
  flex-direction: column;
  transition: transform 380ms cubic-bezier(.32,.72,.16,1);
  z-index: 50;
  overflow: hidden;
  box-shadow: 0 -20px 50px -20px rgba(31, 24, 20, 0.2);

  ${(p) => p.$open && `transform: translate(-50%, 0);`}

  @media (min-width: 720px) {
    height: 86dvh;
    max-height: 820px;
    bottom: 50%;
    border-radius: 28px;
    transform: translate(-50%, calc(50% + 60px));
    opacity: 0;
    pointer-events: none;
    transition: transform 320ms cubic-bezier(.32,.72,.16,1), opacity 260ms ease;
    box-shadow: 0 30px 80px -20px rgba(31, 24, 20, 0.25);

    ${(p) => p.$open && `
      transform: translate(-50%, 50%);
      opacity: 1;
      pointer-events: auto;
    `}
  }
`;

export const SheetHandle = styled.div`
  width: 40px;
  height: 4px;
  background: var(--border-strong);
  border-radius: 99px;
  margin: 10px auto 6px;

  @media (min-width: 720px) { display: none; }
`;

export const SheetHdr = styled.header`
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 8px 20px 14px;
`;

export const SheetBack = styled.button`
  width: 40px;
  height: 40px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--ink);
  flex-shrink: 0;
  box-shadow: var(--shadow-sm);
  transition: transform 160ms ease;

  &:hover { transform: scale(1.06); }
`;

export const SheetTitle = styled.div`
  display: flex;
  flex-direction: column;
  gap: 2px;
`;

export const SheetHeadline = styled.div`
  font-size: 17px;
  font-weight: 700;
  letter-spacing: -0.02em;
`;

export const SheetBody = styled.div`
  flex: 1;
  overflow-y: auto;
  padding: 10px 20px 32px;
  scrollbar-width: thin;
  scrollbar-color: rgba(31, 24, 20, 0.12) transparent;

  &::-webkit-scrollbar { width: 6px; }
  &::-webkit-scrollbar-thumb { background: rgba(31, 24, 20, 0.12); border-radius: 4px; }
`;

// .eyebrow — small label above a sheet headline.
export const Eyebrow = styled.div`
  font-size: 12px;
  letter-spacing: 0.01em;
  color: var(--ink-2);
  font-weight: 600;
`;
