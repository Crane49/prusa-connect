import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { fakeSlicer, t } from '../../test/fakes';
import SetAsCurrent from './set-as-current.svelte';

describe('Set as current', () => {
  it('asks the slicer to select the printer by its id', async () => {
    const fake = fakeSlicer();
    await render(SetAsCurrent, {
      props: { deviceId: 'dev-7', t, look: 'header', slicer: fake.slicer },
    });

    await page.getByRole('button', { name: 'Set as current' }).click();

    expect(fake.sent).toEqual([{ action: 'SELECT_PRINTER', uuid: 'dev-7' }]);
  });

  it('draws nothing outside PrusaSlicer', async () => {
    const fake = fakeSlicer(false);
    const { container } = await render(SetAsCurrent, {
      props: { deviceId: 'dev-7', t, look: 'header', slicer: fake.slicer },
    });

    await expect
      .element(page.getByRole('button', { name: 'Set as current' }))
      .not.toBeInTheDocument();
    expect(container.textContent).toBe('');
  });

  // On the listing the control is one icon button, named by its label, as
  // the cloud's iconOnly. On the page it is a button with its words.
  it('draws an icon button on a listed printer and a worded one on the page', async () => {
    const fake = fakeSlicer();
    await render(SetAsCurrent, {
      props: { deviceId: 'a', t, look: 'row', slicer: fake.slicer },
    });
    await render(SetAsCurrent, {
      props: { deviceId: 'b', t, look: 'header', slicer: fake.slicer },
    });

    const [row, header] = page
      .getByRole('button', { name: 'Set as current' })
      .elements();
    expect(row?.textContent.trim()).toBe('');
    expect(header?.textContent.trim()).toBe('Set as current');

    await page.getByRole('button', { name: 'Set as current' }).first().click();
    expect(fake.sent).toEqual([{ action: 'SELECT_PRINTER', uuid: 'a' }]);
  });
});
