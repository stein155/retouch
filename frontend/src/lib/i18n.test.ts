import { describe, it, expect } from 'vitest';
import { makeT, isSupportedLang, DEFAULT_LANG, I18N, LANGS } from './i18n';

describe('makeT', () => {
  it('returns strings in the requested language', () => {
    const t = makeT('en');
    expect(t('settings')).toBe('Settings');
    expect(t('live')).toBe('LIVE');
  });

  it('translates the same key differently per language', () => {
    expect(makeT('nl')('settings')).toBe('Instellingen');
    expect(makeT('de')('settings')).toBe('Einstellungen');
    expect(makeT('fr')('settings')).toBe('Réglages');
  });

  it('falls back to Dutch for an unknown language', () => {
    const t = makeT('xx');
    expect(t('settings')).toBe(makeT(DEFAULT_LANG)('settings'));
  });

  it('returns the key itself when it is unknown in every dictionary', () => {
    expect(makeT('en')('nope_not_a_key')).toBe('nope_not_a_key');
  });
});

describe('dictionaries', () => {
  it('every language ships every key (no silent fallbacks)', () => {
    const langs = LANGS.map((l) => l.code);
    const reference = Object.keys(I18N[DEFAULT_LANG]).sort();
    for (const lang of langs) {
      expect(Object.keys(I18N[lang as keyof typeof I18N]).sort(), `keys for ${lang}`).toEqual(reference);
    }
  });
});

describe('isSupportedLang', () => {
  it('accepts shipped languages and rejects others', () => {
    expect(isSupportedLang('en')).toBe(true);
    expect(isSupportedLang('nl')).toBe(true);
    expect(isSupportedLang('zz')).toBe(false);
  });
});
