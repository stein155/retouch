import { render } from '@testing-library/react';
import { ThemeProvider } from 'styled-components';
import { theme } from '../theme/theme';
import { I18nContext, makeT } from '../lib/i18n';

// Render a component inside the styled-components theme. Pass { lang } to also
// wrap an I18n provider (defaults to English so assertions read in English).
export function renderWithTheme(ui, { lang = 'en', ...options } = {}) {
  const t = makeT(lang);
  const Wrapper = ({ children }) => (
    <ThemeProvider theme={theme}>
      <I18nContext.Provider value={{ t, lang }}>{children}</I18nContext.Provider>
    </ThemeProvider>
  );
  return render(ui, { wrapper: Wrapper, ...options });
}

export * from '@testing-library/react';
