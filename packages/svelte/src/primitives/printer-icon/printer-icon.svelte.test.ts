import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import { DECLARED_MODELS, type DeclaredModels } from './declared';
import PrinterIcon from './printer-icon.svelte';
import { PRINTER_MODELS, type PrinterIconProps } from './types';
import { printerModelLabel } from './names';

const drawn = async (props: PrinterIconProps): Promise<HTMLImageElement> => {
  const { container } = await render(PrinterIcon, { props });
  const image = container.querySelector('img');
  if (image === null) throw new Error('no icon rendered');
  return image;
};

describe('PrinterIcon', () => {
  it('draws every model from its own file', async () => {
    const files = new Set<string>();

    for (const model of PRINTER_MODELS) {
      const image = await drawn({ model });
      files.add(image.getAttribute('src') ?? '');
    }

    // One file per model. A model that fell back to another's artwork would
    // draw the wrong machine and collapse this set.
    expect(files.size).toBe(PRINTER_MODELS.length);
    for (const file of files)
      expect(file).toMatch(/^\/img\/printers\/.+\.(?:svg|png)$/u);
  });

  it('names the machine, because the icon is how a card identifies it', async () => {
    for (const model of PRINTER_MODELS) {
      const image = await drawn({ model });
      expect(image.alt.trim()).not.toBe('');
    }

    expect((await drawn({ model: 'mk4s' })).alt).toBe('Original Prusa MK4S');
    expect((await drawn({ model: 'xlmultitool3' })).alt).toBe(
      'Original Prusa XL, 3 tool heads',
    );
  });

  it('reads the unrecognised machine out of the catalogue', async () => {
    const image = await drawn({ model: 'unknown' });

    // A product name is the same in every language and stays as it is. A
    // machine that names no product has no name to keep, so both of its words
    // are translated (frontend-i18n.md rule 10). The English is asserted
    // rather than compared against `t()` of the same key, which would pass on
    // a key no catalogue holds.
    expect(image.alt).toBe('Unrecognised printer model');
    expect(printerModelLabel('unknown')).toBe('Unknown model');
    expect(printerModelLabel('mk4smmu3')).toBe('MK4S MMU3');
  });

  it('draws and names a machine a driver declared when it has no artwork for it', async () => {
    const lookup: DeclaredModels = (reported) =>
      reported === 'Bench_One'
        ? {
            name: 'Bench Works Tool Changer',
            src: '/api/v1/vocabularies/device-models/Bench_One/artwork',
          }
        : undefined;
    const declared = async (props: PrinterIconProps) => {
      const { container } = await render(PrinterIcon, {
        props,
        context: new Map([[DECLARED_MODELS, lookup]]),
      });
      return container.querySelector('img');
    };

    const bench = await declared({ model: 'unknown', reported: 'Bench_One' });
    expect(bench?.alt).toBe('Bench Works Tool Changer');
    expect(bench?.getAttribute('src')).toBe(
      '/api/v1/vocabularies/device-models/Bench_One/artwork',
    );
    // A machine with artwork keeps the drawing and the name the library holds.
    const prusa = await declared({ model: 'mk4s', reported: 'Bench_One' });
    expect(prusa?.alt).toBe('Original Prusa MK4S');
    expect(prusa?.getAttribute('src')).toBe('/img/printers/mk4s.svg');
    expect(
      (await declared({ model: 'unknown', reported: 'Somebody' }))?.alt,
    ).toBe('Unrecognised printer model');
  });

  it('draws a declared machine as unknown when its drawing does not load', async () => {
    const { container } = await render(PrinterIcon, {
      props: { model: 'unknown', reported: 'Bench_One' },
      context: new Map([
        [
          DECLARED_MODELS,
          (() => ({
            name: 'Bench Works Tool Changer',
            src: '/no/such/drawing.svg',
          })) satisfies DeclaredModels,
        ],
      ]),
    });
    const image = container.querySelector('img');

    await expect
      .poll(() => image?.getAttribute('src'))
      .toBe('/img/printers/unknown.svg');
    expect(image?.alt).toBe('Bench Works Tool Changer');
  });

  // A server that is down answers neither drawing. The icon then settles on
  // one source, and does not go on asking for the two in turn.
  it('stops asking when the fallback does not load either', async () => {
    const { container } = await render(PrinterIcon, {
      props: { model: 'unknown', reported: 'Bench_One', base: '/no/base' },
      context: new Map([
        [
          DECLARED_MODELS,
          (() => ({
            name: 'Bench Works Tool Changer',
            src: '/no/such/drawing.svg',
          })) satisfies DeclaredModels,
        ],
      ]),
    });
    const image = container.querySelector('img');
    if (image === null) throw new Error('no icon rendered');

    let changes = 0;
    new MutationObserver((records) => {
      changes += records.length;
    }).observe(image, { attributes: true, attributeFilter: ['src'] });

    await new Promise((settled) => setTimeout(settled, 1000));

    expect(changes).toBeLessThanOrEqual(1);
    expect(image.getAttribute('src')).toBe('/no/base/unknown.svg');
  });

  it('serves the artwork from where the page puts it', async () => {
    const image = await drawn({ model: 'mini', base: '/modules/spools/art' });

    expect(image.getAttribute('src')).toBe('/modules/spools/art/mini.svg');
  });

  it('keeps its shape in a box of any ratio', async () => {
    const image = await drawn({ model: 'xl', class: 'size-14' });

    expect(getComputedStyle(image).objectFit).toBe('contain');
    expect(getComputedStyle(image).flexShrink).toBe('0');
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(PrinterIcon, {
      props: { model: 'mk4s', class: 'size-14' },
    });

    await expectNoViolations(container);
  });
});
