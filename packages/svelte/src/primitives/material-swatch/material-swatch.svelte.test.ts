import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import MaterialSwatch from './material-swatch.svelte';
import type { MaterialSwatchProps } from './types';

const draw = async (
  props: Partial<MaterialSwatchProps> = {},
): Promise<HTMLElement> => {
  const { container } = await render(MaterialSwatch, {
    props: {
      material: { name: 'PLA Night Black' },
      class: 'size-10',
      ...props,
    },
  });

  return container;
};

const swatch = (container: HTMLElement): HTMLElement => {
  const found = container.querySelector<HTMLElement>(
    '[data-slot="material-swatch"]',
  );
  if (found === null) throw new Error('no swatch rendered');
  return found;
};

describe('MaterialSwatch', () => {
  it('fills the box with the colour and names it', async () => {
    const container = await draw({
      material: {
        name: 'PETG Slate',
        colorName: 'Slate',
        colorRgba: 'rgb(107, 111, 115)',
      },
    });

    expect(swatch(container).getAttribute('role')).toBe('img');
    expect(swatch(container).getAttribute('aria-label')).toBe('Slate');
    expect(getComputedStyle(swatch(container)).backgroundColor).toBe(
      'rgb(107, 111, 115)',
    );
    await expectNoViolations(container);
  });

  it('names a colour with no name of its own in words', async () => {
    const container = await draw({
      material: { name: 'Generic PLA', colorRgba: '#6b6f73' },
    });

    expect(swatch(container).getAttribute('aria-label')).toBe(
      'Filament colour',
    );
  });

  it('says so, rather than drawing a plausible grey, when it knows nothing', async () => {
    const container = await draw({ material: { name: 'Generic ABS' } });

    expect(swatch(container).getAttribute('aria-label')).toBe(
      'No colour recorded',
    );
    expect(swatch(container).classList.contains('border-dashed')).toBe(true);
    await expectNoViolations(container);
  });

  it('renders no control, so it may sit inside a row link', async () => {
    const container = await draw({
      material: { name: 'PLA', colorRgba: '#1b1b1f' },
    });

    expect(container.querySelector('button')).toBeNull();
  });
});
