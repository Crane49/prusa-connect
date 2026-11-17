import { degreesFor, formatNumber } from '../../i18n';

/** The reading as a person reads it. */
export const wholeDegrees = (value: number): string =>
  formatNumber(degreesFor(value), { maximumFractionDigits: 0 });
