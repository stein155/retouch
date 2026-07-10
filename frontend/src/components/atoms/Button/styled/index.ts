import styled, { css } from 'styled-components';

// Button atom. Variants map verbatim to the original .btn-primary / .btn-ghost /
// .btn-delete / .update-btn rules from styles.css. Pick a variant via the
// $variant prop; default is "primary".

export const primary = css`
  flex: 1;
  background: var(--accent);
  color: #fff;
  border-radius: 14px;
  padding: 16px;
  font-size: 15px;
  font-weight: 700;
  letter-spacing: -0.01em;
  box-shadow: 0 8px 18px -6px color-mix(in oklab, var(--accent) 60%, transparent);
  transition: transform 160ms ease, box-shadow 160ms ease, opacity 160ms ease;

  &:hover:not(:disabled) { transform: translateY(-1px); }
  &:active:not(:disabled) { transform: translateY(0) scale(0.99); }
  &:disabled { opacity: 0.38; box-shadow: none; cursor: default; }
`;

export const ghost = css`
  padding: 16px 20px;
  border-radius: 14px;
  font-weight: 600;
  font-size: 15px;
  color: var(--ink-2);
  background: var(--surface);
  box-shadow: var(--shadow-sm);
  transition: color 160ms ease, box-shadow 160ms ease;

  &:hover { color: var(--ink); box-shadow: var(--shadow); }
`;

export const del = css`
  width: 100%;
  margin-top: 14px;
  padding: 15px;
  border-radius: 14px;
  color: #d44030;
  font-weight: 600;
  font-size: 14px;
  background: transparent;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  transition: background 160ms ease;

  &:hover { background: rgba(212, 64, 48, 0.08); }
`;

export const update = css`
  margin-top: 12px;
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 9px;
  font-size: 15px;
  font-weight: 700;
  color: #fff;
  background: var(--accent);
  border: none;
  border-radius: 14px;
  padding: 14px 16px;
  box-shadow: var(--shadow-sm);
  cursor: pointer;
  transition: background 0.15s ease, opacity 0.15s ease;

  &:hover { background: var(--accent-2); }
  &:disabled { opacity: 0.6; cursor: default; }
`;

const variants = { primary, ghost, delete: del, update };

export const Button = styled.button`
  ${(p) => variants[p.$variant] || primary}
`;
