import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import PrintSectionProbe from '../test/print-section-probe.svelte';

describe('printDialogContext', () => {
  // A module's own test draws its section with no dialog around it.
  it('reads nothing ticked and closes nothing outside a print dialog', async () => {
    await render(PrintSectionProbe, { props: { fileRef: 'local:a.bgcode' } });

    await expect
      .element(page.getByTestId('print-section'))
      .toHaveTextContent('local:a.bgcode for nothing');
    await page.getByRole('button', { name: 'Close from section' }).click();
  });
});
