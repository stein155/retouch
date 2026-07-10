import styled from 'styled-components';

export const Browse = styled.button`
  margin-top: 6px;
  width: 100%;
  display: flex;
  align-items: center;
  gap: 14px;
  background: var(--surface);
  border-radius: var(--radius);
  padding: 14px 16px;
  color: var(--ink);
  transition: transform 180ms cubic-bezier(.34,.78,.18,1), box-shadow 200ms ease;
  box-shadow: var(--shadow-sm);

  &:hover { transform: translateY(-2px); box-shadow: var(--shadow); }
  &:active { transform: translateY(0) scale(0.99); }
  &:disabled { opacity: 0.5; cursor: default; transform: none; box-shadow: var(--shadow-sm); }

  @media (prefers-reduced-motion: reduce) { transition: none; }
`;

export const BrowseIcon = styled.span`
  width: 42px;
  height: 42px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: var(--accent-soft);
  color: var(--accent);
  flex-shrink: 0;
`;

export const BrowseText = styled.span`
  flex: 1;
  display: flex;
  flex-direction: column;
  text-align: left;
`;

export const BrowseTitle = styled.span`
  font-size: 15px;
  font-weight: 700;
  letter-spacing: -0.015em;
`;

export const BrowseSub = styled.span`
  font-size: 12px;
  color: var(--ink-2);
  margin-top: 1px;
  font-weight: 500;
`;

export const BrowseCta = styled.span`
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: var(--surface-3);
  color: var(--ink);
  display: grid;
  place-items: center;
  font-size: 14px;
  flex-shrink: 0;
`;
