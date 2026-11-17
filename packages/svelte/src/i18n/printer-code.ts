// A printer's error code, as a person reads it.
//
// A Prusa printer raises some dialogs with a code and no words: 13807 after an
// upload is stopped. Prusa-Error-Codes names every code, and
// `make generate-printer-error-codes` writes those names to
// `locales/en.printerCode.json`, so the words are in the catalogue and a
// translator gives them in another language under the same key.
//
// A code is a printer number and three digits, and most codes read the same on
// every printer. Those words are written once, under `XX` and the three
// digits. Where some printers say it differently, as the MINI does on its
// small screen, their words are written once too, with the printers in the
// key: `XX301.p17_41` is what the XL and the XLP say. Those are read first.
import { CORE_CATALOGUES } from './catalogues';
import { CORE_NAMESPACE, locale } from './locale.svelte';
import { t } from './translate';

/** What a screen draws for a printer's code. */
export interface PrinterCodeText {
  title: string;
  /**
   * Left out where the firmware fills the sentence in as it shows it, "Dock
   * %d": the catalogue holds the unfilled one, which says nothing here.
   */
  text?: string;
}

/** A printer number and the three digits that say which error it is. */
const CODE = /^(\d{2})(\d{3})$/;

/**
 * The MMU's number. Its codes are a catalogue of their own, so 04101 and 13101
 * are different errors and an MMU code is never read as a printer's.
 */
const MMU = '04';

/** A key of words some printers have of their own: `XX301.p17_41`. */
const OWN_WORDS = /^printerCode\.xbuddy\.(XX(\d{3})\.p([\d_]+))\.title$/;

/** The key of a printer's own words, by the code the printer sends. */
const ownWords = new Map<string, string>();

// English holds every key, so its keys say which printers have their own.
for (const key of Object.keys(CORE_CATALOGUES.en ?? {})) {
  const [, name, error, printers] = OWN_WORDS.exec(key) ?? [];
  if (name === undefined || error === undefined) continue;

  for (const printer of (printers ?? '').split('_')) {
    ownWords.set(printer + error, name);
  }
}

const held = (key: string): boolean =>
  locale.message(CORE_NAMESPACE, key) !== undefined;

/**
 * The title and the text of one code, in the language on screen, or
 * `undefined` for a code the catalogue does not name. A newer firmware raises
 * codes this build has not heard of, and the screen then shows the number.
 */
export const describePrinterCode = (
  code: string | undefined,
): PrinterCodeText | undefined => {
  const parts = code === undefined ? null : CODE.exec(code);
  if (parts === null) return undefined;

  // The printer's own words first, and the ones every printer has after them.
  // An MMU code is written under its own number.
  const candidates = [
    `printerCode.xbuddy.${ownWords.get(parts[0]) ?? parts[0]}`,
  ];
  if (parts[1] !== MMU)
    candidates.push(`printerCode.xbuddy.XX${parts[2] ?? ''}`);

  const key = candidates.find((one) => held(`${one}.title`));
  if (key === undefined) return undefined;

  return {
    title: t(`${key}.title`),
    ...(held(`${key}.text`) && { text: t(`${key}.text`) }),
  };
};

/**
 * The label of one answer the printer offers, in the language on screen, or
 * `undefined` for a name the catalogue does not hold. The name itself, `Retry`,
 * is what the printer is sent back, so only the label is translated.
 */
export const printerButtonLabel = (button: string): string | undefined => {
  const key = `printerButton.xbuddy.${button}`;

  return held(key) ? t(key) : undefined;
};
