import { createGlobalStyle } from 'styled-components';

// Global reset + html/body rules + element resets, copied verbatim from the
// original styles.css. The :root custom properties are kept too so any remaining
// var(--token) reference (e.g. inline styles like color: 'var(--ink)') resolves
// exactly as before.
export const GlobalStyle = createGlobalStyle`
  :root {
    --bg: #f1e6cb;
    --surface: #ffffff;
    --surface-2: #f8ecd0;
    --surface-3: #ead9b3;
    --border: rgba(31, 24, 20, 0.08);
    --border-strong: rgba(31, 24, 20, 0.16);
    --ink: #1f1814;
    --ink-2: #5e4f44;
    --ink-3: #9a8a7a;
    --accent: #c47a52;
    --accent-2: #d99474;
    --accent-soft: #f4dcc6;
    --radius: 20px;
    --radius-lg: 28px;
    --radius-sm: 12px;
    --shell-max: 460px;
    --shadow-sm: 0 1px 2px rgba(31, 24, 20, 0.05), 0 0 0 1px rgba(31, 24, 20, 0.04);
    --shadow: 0 8px 24px -8px rgba(31, 24, 20, 0.14), 0 0 0 1px rgba(31, 24, 20, 0.04);
    --shadow-lg: 0 22px 50px -16px rgba(31, 24, 20, 0.2);
  }

  * { box-sizing: border-box; }
  *, *::before, *::after { user-select: none; -webkit-user-select: none; }

  html, body {
    margin: 0;
    padding: 0;
    background: var(--bg);
    color: var(--ink);
    font-family: 'Plus Jakarta Sans', -apple-system, sans-serif;
    -webkit-font-smoothing: antialiased;
    -moz-osx-font-smoothing: grayscale;
    height: 100dvh;
    overflow: hidden;
    overscroll-behavior: none;
    touch-action: pan-y;
  }

  body {
    background: var(--bg);
  }

  button { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
  input  { font: inherit; color: inherit; user-select: text; -webkit-user-select: text; }
  ::selection { background: var(--accent); color: #fff; }

  @media (min-width: 720px) {
    body {
      display: grid;
      place-items: center;
      overflow: hidden;
    }
  }
`;

export default GlobalStyle;
