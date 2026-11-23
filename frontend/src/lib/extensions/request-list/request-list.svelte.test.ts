import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import RequestList from './request-list.svelte';

describe('RequestList data folder', () => {
  it('names the data folder by the manifest name and says Remove keeps it', async () => {
    await render(RequestList, {
      props: {
        requests: [{ kind: 'data' }],
        instance: 'buddy-debug-logs',
        label: 'This extension asks for:',
      },
    });

    const line = page.getByText(/module-data\/buddy-debug-logs,? .*Removing/);
    await expect.element(line).toBeVisible();
    await expect
      .element(page.getByText(/com\.github\.prusa3d/))
      .not.toBeInTheDocument();
  });
});

describe('RequestList request header', () => {
  it('names the header and gives the reason the module gives', async () => {
    await render(RequestList, {
      props: {
        requests: [
          {
            kind: 'header',
            name: 'Print-After-Upload',
            reason: 'PrusaSlicer 2 says whether to print the file it uploads',
            required: false,
          },
        ],
        instance: 'prusaslicer2-compat',
        label: 'This extension asks for:',
      },
    });

    await expect
      .element(
        page.getByText(
          'Reads the Print-After-Upload header of a request: PrusaSlicer 2 says whether to print the file it uploads.',
        ),
      )
      .toBeVisible();
  });

  it('ends the line with one full stop when the reason is a sentence', async () => {
    await render(RequestList, {
      props: {
        requests: [
          {
            kind: 'header',
            name: 'Print-After-Upload',
            reason: 'PrusaSlicer says whether to print the file.',
            required: false,
          },
        ],
        instance: 'prusaslicer2-compat',
        label: 'This extension asks for:',
      },
    });

    await expect
      .element(
        page.getByText(
          'Reads the Print-After-Upload header of a request: PrusaSlicer says whether to print the file.',
          { exact: true },
        ),
      )
      .toBeVisible();
  });

  it('says the extension does not start without a required header', async () => {
    await render(RequestList, {
      props: {
        requests: [
          {
            kind: 'header',
            name: 'Print-After-Upload',
            reason: 'PrusaSlicer says whether to print the file.',
            required: true,
          },
        ],
        instance: 'prusaslicer2-compat',
        label: 'This extension asks for:',
      },
    });

    await expect
      .element(
        page.getByText(
          'Reads the Print-After-Upload header of a request, and does not start without it: PrusaSlicer says whether to print the file.',
          { exact: true },
        ),
      )
      .toBeVisible();
  });
});
