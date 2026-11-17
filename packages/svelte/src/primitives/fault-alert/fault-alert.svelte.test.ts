import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ReportHarness from '../../test/fault-alert-report-harness.svelte';
import FaultAlert from './fault-alert.svelte';

const fault = {
  code: undefined,
  title: 'Something failed',
  detail: undefined,
  traceId: undefined,
};

describe('FaultAlert report link', () => {
  it('is absent when the shell set no address', async () => {
    await render(FaultAlert, { props: { fault } });

    await expect.element(page.getByText('Something failed')).toBeVisible();
    expect(document.querySelector('a')).toBeNull();
  });

  it('points where the shell said, even on a fault with nothing else to say', async () => {
    await render(ReportHarness, { props: { fault } });

    await expect
      .element(page.getByRole('link', { name: 'Report a problem' }))
      .toHaveAttribute('href', '/settings/diagnostics');
  });
});
