import type { Translate } from '@prusa/connect-ui/i18n';

/**
 * How a page names the person behind a record. The module records the
 * subject the host named, such as `user:alice`. The page shows the account's
 * name without the type, and says "you" for the person reading.
 */
export const personOf = (
  subject: string,
  who: string,
  t: Translate,
): string => {
  if (subject === who) return t('parts.person.you');

  const [, name] = subject.split(':', 2);

  return name === undefined || name === '' ? subject : name;
};
