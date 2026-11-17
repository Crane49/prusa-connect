import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import Art from './art.svelte';
import { ART_NAMES } from './types';
import { ART_ALT_KEY, ART_FILE, ART_RATIO } from './variants';

describe('Art', () => {
  it('says what the drawing is of', async () => {
    await render(Art, { props: { name: 'empty-fleet' } });

    await expect
      .element(
        page.getByRole('img', {
          name: 'A printer on a desk, its cable running off the edge',
        }),
      )
      .toHaveAttribute('data-variant', 'empty-fleet');
  });

  it('reads its artwork from the folder it is given', async () => {
    const { container } = await render(Art, {
      props: { name: 'fractal', base: '/assets/art' },
    });

    const drawing = container.querySelector('[data-slot="art"]');
    expect(drawing?.getAttribute('style')).toContain(
      "url('/assets/art/fractal.svg')",
    );
  });

  it('keeps the drawing in its own box', async () => {
    const { container } = await render(Art, { props: { name: 'shelf' } });

    const drawing = container.querySelector('[data-slot="art"]');
    expect(drawing?.getAttribute('style')).toContain('--art-ratio: 240 / 24');
  });

  it('hides a drawing that is texture and not a picture', async () => {
    const { container } = await render(Art, {
      props: { name: 'camera-grid', decorative: true, fit: 'tile' },
    });

    const drawing = container.querySelector('[data-slot="art"]');
    expect(drawing?.getAttribute('aria-hidden')).toBe('true');
    expect(drawing?.getAttribute('role')).toBeNull();
    expect(drawing?.getAttribute('data-fit')).toBe('tile');
  });

  it('takes an alt from the caller, for a screen that knows more', async () => {
    await render(Art, {
      props: { name: 'fractal', alt: 'Two servers linked to this one' },
    });

    await expect
      .element(
        page.getByRole('img', { name: 'Two servers linked to this one' }),
      )
      .toBeInTheDocument();
  });

  it('has a file, a box and a name for every drawing', () => {
    for (const name of ART_NAMES) {
      expect(ART_FILE[name]).toMatch(/\.svg$/);
      expect(ART_RATIO[name]).not.toBe('');
      expect(ART_ALT_KEY[name]).toMatch(/^art\./);
    }
  });
});
