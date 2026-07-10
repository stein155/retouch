import styled from 'styled-components';

// .spk-knob
export const Knob = styled.span`
  position: absolute;
  top: 3px;
  left: 3px;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: #fff;
  box-shadow: var(--shadow-sm);
  transition: transform 0.18s ease;

  @media (prefers-reduced-motion: reduce) { transition: none; }
`;

// .spk-toggle — on/off switch. $on maps to .is-on.
export const Switch = styled.button`
  flex-shrink: 0;
  width: 46px;
  height: 28px;
  padding: 0;
  border: none;
  border-radius: 99px;
  cursor: pointer;
  background: var(--border-strong);
  position: relative;
  transition: background 0.18s ease;

  ${(p) => p.$on && `background: var(--accent);`}
  &:disabled { opacity: 0.6; cursor: default; }
  ${(p) => p.$on && `${Knob} { transform: translateX(18px); }`}

  @media (prefers-reduced-motion: reduce) { transition: none; }
`;
