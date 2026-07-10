import { useCallback, useEffect, useState } from 'react';

// Colour-scheme handling. The preference is one of 'system' | 'light' | 'dark'
// and is stored per device in localStorage (the same client-only pattern as the
// beta-updates flag — no /api/settings round-trip). Whatever the preference, a
// *concrete* theme ('light' | 'dark') is written to the data-theme attribute on
// <html>, which is what GlobalStyle's [data-theme='dark'] rules key off. In
// 'system' mode that concrete value tracks the OS via prefers-color-scheme and
// updates live when the OS setting changes.
//
// Keep the storage key + resolution logic in sync with the inline pre-paint
// script in index.html, which applies the same attribute before the bundle
// loads so there's no light-mode flash on a dark device.

export type ThemeMode = 'system' | 'light' | 'dark';
export type ConcreteTheme = 'light' | 'dark';

export const THEME_KEY = 'retouch-theme';
export const THEME_MODES: ThemeMode[] = ['system', 'light', 'dark'];

const isMode = (v: unknown): v is ThemeMode => THEME_MODES.includes(v as ThemeMode);

const media = (): MediaQueryList | null =>
  typeof window !== 'undefined' && window.matchMedia
    ? window.matchMedia('(prefers-color-scheme: dark)')
    : null;

export function getStoredMode(): ThemeMode {
  try {
    const v = localStorage.getItem(THEME_KEY);
    return isMode(v) ? v : 'system';
  } catch {
    return 'system';
  }
}

// Resolve a preference to the concrete theme that should be painted.
export function resolveTheme(mode: ThemeMode): ConcreteTheme {
  if (mode === 'light' || mode === 'dark') return mode;
  const m = media();
  return m && m.matches ? 'dark' : 'light';
}

// Write the concrete theme to <html data-theme> and keep the browser chrome
// (address bar / status bar) colour in step via the theme-color meta tag.
export function applyTheme(mode: ThemeMode) {
  if (typeof document === 'undefined') return;
  const theme = resolveTheme(mode);
  document.documentElement.setAttribute('data-theme', theme);
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute('content', theme === 'dark' ? '#17130f' : '#f1e6cb');
}

// Hook: returns [mode, setMode]. Applies the theme on mount and whenever the
// preference changes, and — while in 'system' mode — re-applies when the OS
// scheme flips.
export function useThemeMode() {
  const [mode, setMode] = useState(getStoredMode);

  useEffect(() => {
    applyTheme(mode);
    try { localStorage.setItem(THEME_KEY, mode); } catch { /* private mode: skip */ }
    if (mode !== 'system') return undefined;
    const m = media();
    if (!m) return undefined;
    const onChange = () => applyTheme('system');
    m.addEventListener('change', onChange);
    return () => m.removeEventListener('change', onChange);
  }, [mode]);

  const set = useCallback((next: string) => {
    setMode(isMode(next) ? next : 'system');
  }, []);

  return [mode, set] as const;
}
