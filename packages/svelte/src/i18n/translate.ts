// `t()`, and the namespace-bound `t` a module receives.
import { CORE_NAMESPACE, locale } from './locale.svelte';
import { format, type MessageParams } from './messages';

/** What every text on the screen is written as. */
export type Translate = (key: string, params?: MessageParams) => string;

/**
 * One text, in the language on screen.
 *
 * The key says where the text is used and not what it says, so a reworded
 * message keeps its key and every translation with it: `fleet.empty.title`,
 * never `no_printers_yet`. Parameters are named, because the order of the
 * parts of a sentence changes with the language.
 *
 * ```svelte
 * <h1>{t('fleet.title')}</h1>
 * <p>{t('device.printing', { name: device.name })}</p>
 * <p>{t('fleet.count', { count: devices.length })}</p>
 * ```
 *
 * A key missing from the chosen language falls back to English. A key no
 * catalogue holds is recorded on `locale.missing` and drawn as itself, so the
 * fault is on the screen where somebody will fix it rather than hidden behind
 * a blank.
 */
export const translate = (
  namespace: string,
  key: string,
  params?: MessageParams,
): string => {
  const message = locale.message(namespace, key);

  if (message === undefined) {
    locale.record(key);

    return key;
  }

  return format(message, params, locale.current);
};

/** The core's own texts: every screen, every component in this library. */
export const t: Translate = (key, params) =>
  translate(CORE_NAMESPACE, key, params);

/**
 * A `t` that reads one module's catalogue first and the core's after it. The
 * loader builds one per module and puts it on the host (ADR-20260911-4p6 rule 18), so
 * a module's page writes `host.t('spools.title')` and never a literal.
 */
export const translateFor =
  (namespace: string): Translate =>
  (key, params) =>
    translate(namespace, key, params);
