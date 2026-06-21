// Theme tokens mirroring the original :root CSS custom properties exactly.
// Provided via styled-components ThemeProvider in main.jsx. Components read these
// through props.theme; GlobalStyle also re-exposes them as CSS variables so any
// remaining var(--token) reference (e.g. inline styles) keeps working.
export const theme = {
  bg: '#f1e6cb',
  surface: '#ffffff',
  surface2: '#f8ecd0',
  surface3: '#ead9b3',
  border: 'rgba(31, 24, 20, 0.08)',
  borderStrong: 'rgba(31, 24, 20, 0.16)',
  ink: '#1f1814',
  ink2: '#5e4f44',
  ink3: '#9a8a7a',
  accent: '#c47a52',
  accent2: '#d99474',
  accentSoft: '#f4dcc6',
  radius: '20px',
  radiusLg: '28px',
  radiusSm: '12px',
  shellMax: '460px',
  shadowSm: '0 1px 2px rgba(31, 24, 20, 0.05), 0 0 0 1px rgba(31, 24, 20, 0.04)',
  shadow: '0 8px 24px -8px rgba(31, 24, 20, 0.14), 0 0 0 1px rgba(31, 24, 20, 0.04)',
  shadowLg: '0 22px 50px -16px rgba(31, 24, 20, 0.2)',
};

export default theme;
