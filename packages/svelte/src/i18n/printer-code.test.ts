import { describe, expect, it } from 'vitest';

import { CORE_CATALOGUES } from './catalogues';
import { describePrinterCode, printerButtonLabel } from './printer-code';

describe('describePrinterCode', () => {
  it('names a code every printer says the same', () => {
    // The words are keyed under XX807, and the MK4 sends 13807.
    expect(describePrinterCode('13807')).toEqual({
      title: 'File error',
      text: 'File error',
    });
    expect(describePrinterCode('36807')?.title).toBe('File error');
    // A printer newer than this build still raises the code every printer has.
    expect(describePrinterCode('99807')?.title).toBe('File error');
  });

  it("reads a printer's own words before the ones every printer has", () => {
    // The MINI shortens the title for its small screen.
    expect(describePrinterCode('12113')?.title).toBe('FS Troubleshooting');
    expect(describePrinterCode('13113')?.title).toBe(
      'Fil. Sensor Troubleshooting',
    );
    // The XL and the XLP share one line, `XX301.p17_41`.
    const xl = describePrinterCode('17301');
    expect(xl?.text).toBe(
      'Failed to home the Z-axis, make sure the loadcell is working.',
    );
    expect(describePrinterCode('41301')).toEqual(xl);
    expect(describePrinterCode('13301')?.text).not.toBe(xl?.text);
  });

  it("keeps the MMU's codes apart from the printers'", () => {
    expect(describePrinterCode('04101')?.title).toBe('FINDA DIDNT TRIGGER');
    expect(describePrinterCode('13101')?.title).toBe('STUCK FILAMENT DETECTED');
    // 807 is a code of the printers, and the MMU has none of that number.
    expect(describePrinterCode('04807')).toBeUndefined();
  });

  it('names nothing for a code the catalogue does not hold', () => {
    expect(describePrinterCode(undefined)).toBeUndefined();
    expect(describePrinterCode('13999')).toBeUndefined();
    expect(describePrinterCode('807')).toBeUndefined();
    expect(describePrinterCode('XX807')).toBeUndefined();
  });

  it('gives the title alone where the firmware fills the text in', () => {
    // "Identity name: %s" says nothing without the name.
    expect(describePrinterCode('13848')).toEqual({
      title: 'UNTRUSTED IDENTITY',
    });
  });

  it('holds a title for every code that has a text', () => {
    const english = CORE_CATALOGUES.en ?? {};
    const untitled = Object.keys(english).filter(
      (key) =>
        key.startsWith('printerCode.') &&
        key.endsWith('.text') &&
        english[key.replace(/\.text$/, '.title')] === undefined,
    );

    expect(untitled).toEqual([]);
  });

  it('holds no text with a place the firmware writes a value into', () => {
    const unfilled = Object.entries(CORE_CATALOGUES.en ?? {}).filter(
      ([key, message]) =>
        key.startsWith('printerCode.') && /%[-0-9.]*[a-zA-Z]/.test(message),
    );

    expect(unfilled).toEqual([]);
  });
});

describe('printerButtonLabel', () => {
  it('labels an answer the firmware names', () => {
    expect(printerButtonLabel('Retry')).toBe('Retry');
    expect(printerButtonLabel('FS_disable')).toBe('Disable FS');
  });

  it('labels nothing for a name the catalogue does not hold', () => {
    expect(printerButtonLabel('Brand_new')).toBeUndefined();
  });
});
