import { formatNumber } from '../../i18n';

/**
 * The picked values as one short string: the first, plus how many more. A
 * facet's trigger says what it is filtered by, so nobody has to reopen a
 * panel to remember.
 */
export const facetListSummary = (values: string[]): string | undefined => {
  const [first] = values;
  if (first === undefined) return undefined;

  return values.length === 1
    ? first
    : `${first} +${formatNumber(values.length - 1)}`;
};
