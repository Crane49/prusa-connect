// Every text on a screen, in the language the person reads.
export { describeFault, type FaultText } from './fault';
export {
  celsiusFor,
  degreesFor,
  formatBytes,
  formatDate,
  formatDateTime,
  formatDuration,
  formatList,
  formatNumber,
  formatRelative,
  formatUnit,
} from './format';
export {
  type Clock,
  CLOCK_STORAGE_KEY,
  CLOCKS,
  formatting,
  type FormattingStorage,
  FormattingStore,
  type FormattingStoreOptions,
  TEMPERATURE_STORAGE_KEY,
  TEMPERATURE_UNITS,
  type TemperatureUnit,
} from './formatting.svelte';
export {
  CORE_NAMESPACE,
  locale,
  type LocaleTag,
  LOCALES,
  localeOf,
  pickLocale,
  SOURCE_LOCALE,
} from './locale.svelte';
export type { MessageParams, Messages } from './messages';
export {
  describePrinterCode,
  printerButtonLabel,
  type PrinterCodeText,
} from './printer-code';
export { t, translate, translateFor, type Translate } from './translate';

import { locale } from './locale.svelte';
import type { Messages } from './messages';

/** Adds one language of one namespace to the page. */
export const registerCatalogue = (
  namespace: string,
  language: string,
  messages: Messages,
): void => {
  locale.register(namespace, language, messages);
};

/** Drops every catalogue of one namespace, for a module the host forgot. */
export const forgetCatalogue = (namespace: string): void => {
  locale.forget(namespace);
};
