import styled from 'styled-components';

// .shell — the phone-sized app frame, centred with a device-like card on desktop.
export const Shell = styled.div`
  /* Definite width, not width:100% — on desktop the body is a centring grid whose
     item shrink-wraps to content, so a percentage width collapses to the content
     width and the frame resizes as content changes. A fixed basis keeps it steady. */
  width: var(--shell-max);
  max-width: 100%;
  height: 100dvh;
  margin: 0 auto;
  position: relative;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg);

  @media (min-width: 720px) {
    height: min(880px, calc(100dvh - 64px));
    max-height: 920px;
    background: var(--bg);
    border-radius: 36px;
    overflow: hidden;
    box-shadow:
      0 0 0 1px rgba(31, 24, 20, 0.06),
      0 30px 60px -20px rgba(31, 24, 20, 0.22),
      0 12px 24px -10px rgba(31, 24, 20, 0.12);
  }
`;

// .shell-scroll — the scrolling body between the header and the mini-player.
export const ShellScroll = styled.div`
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
  overscroll-behavior: contain;
  /* Clear the absolutely-positioned header (see Header's Hdr): its height is
     the safe-area top inset + ~58px, floored at 74px. */
  padding-top: max(74px, calc(env(safe-area-inset-top) + 58px));
  padding-bottom: max(140px, calc(env(safe-area-inset-bottom) + 130px));
  scrollbar-width: thin;
  scrollbar-color: rgba(31, 24, 20, 0.15) transparent;

  &::-webkit-scrollbar { width: 6px; }
  &::-webkit-scrollbar-thumb { background: rgba(31, 24, 20, 0.12); border-radius: 4px; }
`;

// .ftr — footer line under the body content.
export const Footer = styled.footer`
  padding: 24px 22px 18px;
  color: var(--ink-3);
  font-size: 12px;
  text-align: center;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
`;

export const FooterBose = styled.span`
  font-weight: 700;
  letter-spacing: 0.02em;
  color: var(--ink-2);
`;
