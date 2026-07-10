import { createGlobalStyle } from 'styled-components';

// Global reset + html/body rules + element resets, copied verbatim from the
// original styles.css. The :root custom properties are kept too so any remaining
// var(--token) reference (e.g. inline styles like color: 'var(--ink)') resolves
// exactly as before.
export const GlobalStyle = createGlobalStyle`
  :root {
    /* Light theme (default). --bg is the flat base colour; --bg-rgb is the same
       value as raw channels so translucent fades can be composed with rgba();
       --app-bg is the canvas gradient (its top stop equals --bg so the solid
       header strip meets it seamlessly). --shimmer is the light sweep used by
       skeleton loaders — dialled right down in dark mode. */
    --bg: #f1e6cb;
    --bg-rgb: 241, 230, 203;
    --app-bg: linear-gradient(176deg, #f1e6cb 0%, #efe1c1 42%, #e7d7b2 100%);
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
    --shimmer: rgba(255, 255, 255, 0.55);
    --radius: 20px;
    --radius-lg: 28px;
    --radius-sm: 12px;
    --shell-max: 460px;
    --shadow-sm: 0 1px 2px rgba(31, 24, 20, 0.05), 0 0 0 1px rgba(31, 24, 20, 0.04);
    --shadow: 0 8px 24px -8px rgba(31, 24, 20, 0.14), 0 0 0 1px rgba(31, 24, 20, 0.04);
    --shadow-lg: 0 22px 50px -16px rgba(31, 24, 20, 0.2);
  }

  /* Dark theme — warm, so the terracotta identity carries over. Applied by the
     data-theme attribute on <html>, which is set before paint (see index.html)
     and toggled at runtime (see theme/themeMode.js). Shadows stay dark; only the
     highlight tokens (--shimmer, borders) flip to light-on-dark. */
  [data-theme='dark'] {
    --bg: #17130f;
    --bg-rgb: 23, 19, 15;
    --app-bg: linear-gradient(176deg, #211a14 0%, #17130f 48%, #100c09 100%);
    --surface: #221d18;
    --surface-2: #2a2019;
    --surface-3: #362c23;
    --border: rgba(245, 236, 222, 0.09);
    --border-strong: rgba(245, 236, 222, 0.18);
    --ink: #f3e9db;
    --ink-2: #c6b6a4;
    --ink-3: #8c7d6d;
    --accent: #db8c60;
    --accent-2: #e6a482;
    --accent-soft: #3b2a1f;
    --shimmer: rgba(255, 255, 255, 0.06);
    --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.4), 0 0 0 1px rgba(0, 0, 0, 0.3);
    --shadow: 0 8px 24px -8px rgba(0, 0, 0, 0.55), 0 0 0 1px rgba(0, 0, 0, 0.35);
    --shadow-lg: 0 22px 50px -16px rgba(0, 0, 0, 0.65);
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
    background: var(--app-bg);
    background-attachment: fixed;
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
