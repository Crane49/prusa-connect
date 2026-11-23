import {
  type LocaleTag,
  localeOf,
  SOURCE_LOCALE,
} from '@prusa/connect-ui/i18n';

/**
 * The language of this page for the language PrusaSlicer names, such as
 * `cs_CZ`. Catalan is read as Spanish. A language this app ships no catalogue for is English.
 */
export const slicerLanguage = (named: string): LocaleTag => {
  if (named === 'ca_ES') return 'es';

  return localeOf(named.replace('_', '-')) ?? SOURCE_LOCALE;
};
